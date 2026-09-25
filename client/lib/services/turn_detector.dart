// 话轮「说完判定」降级链（voice-turn-detection 1.2/1.3）。
//
// 预注册晋级判据（proposal 修订 2026-09-25）：纯能量 VAD 的标注评估若
// 误判率 >15% 或轮次延迟 p90 >1.2s，则晋级自部署轮次模型。本模块把判定
// 点收拢成纯函数降级链——模型（预留插槽）→ 完形度规则（预留插槽）→ 静默
// 档（实装）——逐帧输入 RMS，输出判定阶段与结论；不碰时钟、不碰采集，
// 评估 harness（client/tool/turn_detection_eval.dart）直接喂标注帧序列。
//
// 帧假设与 duplex_gate 一致：16kHz 采集每个 PCM 块记作一帧（50ms）。
// 本文件保持 flutter 无关（纯 Dart），供 `dart run` 评估脚本复用；
// 与 vad_service 的能量阈值同值，由 flutter 测试锁等值防漂移。

/// 空闲期起判阈值（RMS）：与 VoiceActivityGate.defaultStartThreshold 同值。
const double turnStartRmsThreshold = 0.015;

/// 语音继续阈值（RMS）：与 VoiceActivityGate.defaultContinueThreshold 同值。
const double turnContinueRmsThreshold = 0.006;

/// 单帧时长：16kHz 采集的 50ms 帧节奏。
const int turnFrameMs = 50;

/// 话轮判定阶段，降级链从上到下逐级回退。
enum TurnDetectionStage {
  /// 自部署轮次模型（voice-turn-detection 决策 c 的预留插槽，未接入）。
  model,

  /// 完形度规则（尾词/疑问句式等，预留插槽，未接入）。
  completenessRule,

  /// 静默档：连续静默达到阈值即判完（当前实装的能量线）。
  silence,
}

/// 交给模型/规则阶段的判定上下文：能量线观测到的当前话轮形态。
class TurnDecisionContext {
  /// 自语音确认起的话轮时长。
  final Duration utteranceDuration;

  /// 当前连续真静默（低于 continue 阈值）时长。
  final Duration trailingSilence;

  const TurnDecisionContext({
    required this.utteranceDuration,
    required this.trailingSilence,
  });
}

/// 阶段委托：模型/规则阶段的预留插槽。
///
/// 返回 true=判完（采纳该阶段结论）；false=否决（该阶段认为还没说完，
/// 链停在本轮，静默档重新计满一整段生效阈值后再问）；null=未决（未接入/
/// 超时），链继续下探。
typedef TurnStageDelegate = bool? Function(TurnDecisionContext context);

/// 静默档策略：tuned=调参档（阈值可配/可带语速自适应），fixed=固定档
/// （降级回退，等价历史行为）。两档实为同一静默档，只是阈值来源不同；
/// 回退触发条件即 strategy/fallbackEnabled 两个配置位，由设置层注入。
enum TurnSilenceStrategy { tuned, fixed }

/// 话轮判定参数：全部可配置，不硬编码 1400。
class TurnDetectionParams {
  /// 静默档策略。默认 fixed=固定档，等价 voice-duplex 落地的历史行为。
  final TurnSilenceStrategy strategy;

  /// 调参档静默阈值（strategy=tuned 时生效）。
  final Duration tunedThreshold;

  /// 调参档是否启用语速自适应：话轮内被吸收的犹豫小停顿（≥[minHesitation]
  /// 且短于当前生效阈值）每出现一次，生效阈值上浮 25%，封顶 [adaptiveCap]。
  final bool adaptive;

  /// 语速自适应的阈值上限。
  final Duration adaptiveCap;

  /// 降级固定档阈值（历史基线 1400ms）。
  final Duration fallbackThreshold;

  /// 降级回退开关：模型/规则插槽未决（或未接入）时，静默档是否兜底判完。
  /// 关闭后仅 strategy=tuned 受影响——链停在未判、交给上层超时；
  /// strategy=fixed 或开关开启时静默档始终是链的最后一环。注意 tuned/fixed
  /// 实为同一静默档，只是阈值来源不同，并非链上的两环。
  final bool fallbackEnabled;

  /// 犹豫小停顿的最短时长（低于它的单帧抖动不算犹豫）。
  final Duration minHesitation;

  const TurnDetectionParams({
    this.strategy = TurnSilenceStrategy.fixed,
    this.tunedThreshold = const Duration(milliseconds: 1400),
    this.adaptive = false,
    this.adaptiveCap = const Duration(milliseconds: 2000),
    this.fallbackThreshold = const Duration(milliseconds: 1400),
    this.fallbackEnabled = true,
    this.minHesitation = const Duration(milliseconds: 100),
  });

  /// 当前静默档的基础阈值（自适应前）。
  Duration get baseSilenceThreshold =>
      strategy == TurnSilenceStrategy.tuned ? tunedThreshold : fallbackThreshold;
}

/// 一帧观测后的链上结论。
class TurnObservation {
  /// 当前推进中的判定阶段（发生判定时即结论来源阶段）。
  final TurnDetectionStage stage;

  /// 本帧是否判完（话轮结束）。
  final bool decided;

  /// 是否已确认语音（话轮进行中）。
  final bool speechConfirmed;

  /// 静默档当前生效阈值（调参/自适应展开后，观测与测试用）。
  final Duration effectiveSilenceThreshold;

  const TurnObservation({
    required this.stage,
    required this.decided,
    required this.speechConfirmed,
    required this.effectiveSilenceThreshold,
  });

  static const idle = TurnObservation(
    stage: TurnDetectionStage.silence,
    decided: false,
    speechConfirmed: false,
    effectiveSilenceThreshold: Duration.zero,
  );
}

/// 话轮判定降级链：纯逐帧状态机。
///
/// 语义（与 vad_service 的判句行为同基线）：
/// - 语音确认：连续 ≥2 帧高于 [turnStartRmsThreshold]（对齐
///   VoiceActivityGate.minimumSpeechFrames），确认后低于
///   [turnContinueRmsThreshold] 的帧计静默；
/// - 静默档：连续静默累计达生效阈值 → 判完。调参档开启自适应时，话轮内
///   被吸收的犹豫小停顿逐次抬高生效阈值（+25%，封顶 adaptiveCap）；
/// - 模型/规则插槽：在静默档即将判完的那一帧先行咨询，返回 true 即采纳、
///   false 即否决（本轮不判，静默重新计满再生效）、null 即下探静默档。
class TurnDetectionChain {
  TurnDetectionParams params;

  /// 预留插槽：自部署轮次模型（voice-turn-detection 决策 c 落地时注入）。
  final TurnStageDelegate? modelStage;

  /// 预留插槽：完形度规则（降级链中间档，未接入）。
  final TurnStageDelegate? ruleStage;

  int _startRunFrames = 0;
  int _confirmed = 0;
  int _silenceRunFrames = 0;
  int _hesitations = 0;
  int _utteranceFrames = 0;
  // 插槽否决位点（静默累计毫秒；<0=未被否决）：再累计满一整段生效阈值
  // 才允许二次询问，既不反复刷插槽也不会否决后永久挂起。
  int _vetoedAtSilenceMs = -1;
  bool _decided = false;

  TurnDetectionChain({
    this.params = const TurnDetectionParams(),
    this.modelStage,
    this.ruleStage,
  });

  bool get speechConfirmed => _confirmed > 0;
  bool get decided => _decided;

  /// 静默档当前生效阈值（基础档 × 自适应展开）。
  Duration get effectiveSilenceThreshold {
    final base = params.baseSilenceThreshold;
    if (params.strategy != TurnSilenceStrategy.tuned || !params.adaptive) {
      return base;
    }
    final boostedMs =
        (base.inMilliseconds * (1 + 0.25 * _hesitations)).round();
    final capped = boostedMs > params.adaptiveCap.inMilliseconds
        ? params.adaptiveCap.inMilliseconds
        : boostedMs;
    return Duration(milliseconds: capped);
  }

  /// 逐帧观测一个 PCM 块的 RMS。
  TurnObservation observe(double rms) {
    if (_decided) {
      return TurnObservation(
        stage: TurnDetectionStage.silence,
        decided: true,
        speechConfirmed: speechConfirmed,
        effectiveSilenceThreshold: effectiveSilenceThreshold,
      );
    }
    if (!speechConfirmed) {
      return _observeUnconfirmed(rms);
    }
    _utteranceFrames++;
    if (rms >= turnContinueRmsThreshold) {
      _absorbHesitation();
      return TurnObservation(
        stage: TurnDetectionStage.silence,
        decided: false,
        speechConfirmed: true,
        effectiveSilenceThreshold: effectiveSilenceThreshold,
      );
    }
    _silenceRunFrames++;
    return _observeSilenceRun();
  }

  TurnObservation _observeUnconfirmed(double rms) {
    if (rms >= turnStartRmsThreshold) {
      _startRunFrames++;
      if (_startRunFrames >= 2) {
        _confirmed = 1;
        _silenceRunFrames = 0;
        _hesitations = 0;
        _utteranceFrames = 0;
        _vetoedAtSilenceMs = -1;
      }
    } else {
      _startRunFrames = 0;
    }
    return TurnObservation.idle;
  }

  /// 语音恢复帧：被吸收的静默若构成犹豫小停顿，抬高科技生效阈值。
  void _absorbHesitation() {
    final silentMs = _silenceRunFrames * turnFrameMs;
    if (silentMs >= params.minHesitation.inMilliseconds) {
      _hesitations++;
    }
    _silenceRunFrames = 0;
    _vetoedAtSilenceMs = -1;
  }

  TurnObservation _observeSilenceRun() {
    final threshold = effectiveSilenceThreshold;
    final silenceMs = _silenceRunFrames * turnFrameMs;
    if (silenceMs < threshold.inMilliseconds) {
      return TurnObservation(
        stage: TurnDetectionStage.silence,
        decided: false,
        speechConfirmed: true,
        effectiveSilenceThreshold: threshold,
      );
    }
    if (_vetoedAtSilenceMs >= 0 &&
        silenceMs - _vetoedAtSilenceMs < threshold.inMilliseconds) {
      // 插槽否决后尚未再累计满一段生效阈值：保持不判。
      return TurnObservation(
        stage: TurnDetectionStage.silence,
        decided: false,
        speechConfirmed: true,
        effectiveSilenceThreshold: threshold,
      );
    }
    final context = TurnDecisionContext(
      utteranceDuration: Duration(milliseconds: _utteranceFrames * turnFrameMs),
      trailingSilence: Duration(milliseconds: silenceMs),
    );
    final consulted = _consultStages(context);
    if (consulted == null) {
      if (params.strategy == TurnSilenceStrategy.tuned &&
          !params.fallbackEnabled) {
        // 调参档关闭回退：模型/规则插槽未决（或未接入）时链停在未判，
        // 交给上层超时，不再下探静默档兜底（fallbackEnabled 契约，预注册
        // 降级链 API 的最后一环开关）。
        return TurnObservation(
          stage: TurnDetectionStage.silence,
          decided: false,
          speechConfirmed: true,
          effectiveSilenceThreshold: threshold,
        );
      }
      // 模型/规则未决：静默档兜底判完（降级链最后一环）。
      return _fire(TurnDetectionStage.silence, threshold);
    }
    final (stage, verdict) = consulted;
    if (verdict) {
      return _fire(stage, threshold);
    }
    _vetoedAtSilenceMs = silenceMs;
    return TurnObservation(
      stage: stage,
      decided: false,
      speechConfirmed: true,
      effectiveSilenceThreshold: threshold,
    );
  }

  /// 从上到下咨询插槽：返回给出结论的阶段与其判定；全未决返回 null。
  (TurnDetectionStage, bool)? _consultStages(TurnDecisionContext context) {
    final model = modelStage;
    if (model != null) {
      final verdict = model(context);
      if (verdict != null) return (TurnDetectionStage.model, verdict);
    }
    final rule = ruleStage;
    if (rule != null) {
      final verdict = rule(context);
      if (verdict != null) return (TurnDetectionStage.completenessRule, verdict);
    }
    return null;
  }

  TurnObservation _fire(
    TurnDetectionStage stage,
    Duration threshold,
  ) {
    _decided = true;
    return TurnObservation(
      stage: stage,
      decided: true,
      speechConfirmed: true,
      effectiveSilenceThreshold: threshold,
    );
  }

  /// 话轮收口（判完或被上层打断）后重置，等待下一个话轮。
  void reset() {
    _startRunFrames = 0;
    _confirmed = 0;
    _silenceRunFrames = 0;
    _hesitations = 0;
    _utteranceFrames = 0;
    _vetoedAtSilenceMs = -1;
    _decided = false;
  }
}

/// 纯判定入口：给定一段逐帧 RMS 序列，返回逐帧链上结论。
/// 评估 harness 与测试用它锁行为，无需触碰采集栈。
List<TurnObservation> evaluateTurnDetection(
  List<double> rmsFrames, {
  TurnDetectionParams params = const TurnDetectionParams(),
  TurnStageDelegate? modelStage,
  TurnStageDelegate? ruleStage,
}) {
  final chain = TurnDetectionChain(
    params: params,
    modelStage: modelStage,
    ruleStage: ruleStage,
  );
  return [for (final rms in rmsFrames) chain.observe(rms)];
}
