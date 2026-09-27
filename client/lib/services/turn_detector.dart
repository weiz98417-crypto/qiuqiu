// 话轮「说完判定」降级链（voice-turn-detection 1.2/1.3）。
//
// 预注册晋级判据（proposal 修订 2026-09-25）：纯能量 VAD 的标注评估若
// 误判率 >15% 或轮次延迟 p90 >1.2s，则晋级自部署轮次模型。本模块把判定
// 点收拢成纯函数降级链——模型（远程插槽，决策 c）→ 完形度规则（预留插槽）
// → 静默档（实装）——逐帧输入 RMS，输出判定阶段与结论；不碰时钟、不碰
// 采集，评估 harness（client/tool/turn_detection_eval.dart）直接喂标注帧
// 序列。
//
// 帧假设与 duplex_gate 一致：16kHz 采集每个 PCM 块记作一帧（50ms）。
// 本文件保持 flutter 无关（纯 Dart），供 `dart run` 评估脚本复用；
// 与 vad_service 的能量阈值同值，由 flutter 测试锁等值防漂移。

import 'dart:async';

/// 空闲期起判阈值（RMS）：与 VoiceActivityGate.defaultStartThreshold 同值。
const double turnStartRmsThreshold = 0.015;

/// 语音继续阈值（RMS）：与 VoiceActivityGate.defaultContinueThreshold 同值。
const double turnContinueRmsThreshold = 0.006;

/// 单帧时长：16kHz 采集的 50ms 帧节奏。
const int turnFrameMs = 50;

/// model 插槽提前问门槛（voice-turn-detection 决策 c）：静默累计超过该值
/// 即开始先行咨询远程轮次模型（结果 true 提前判完，赶在静默阈值前）。
/// 值取 600ms——低于生效阈值下限（调参档 800ms），给 500ms 的 sidecar 预算
/// 留出往返余量。
const int turnModelEarlyQueryMs = 600;

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
///   接入远程模型时链可开启提前问（[modelEarlyQueryAfter]）：静默累计
///   超过门槛即先行咨询模型，true 提前判完，其余语义不变。
class TurnDetectionChain {
  TurnDetectionParams params;

  /// model 插槽：自部署轮次模型（voice-turn-detection 决策 c）。运行时
  /// 可注入/摘除（VADService.attachTurnModel），不换链实例。
  TurnStageDelegate? modelStage;

  /// model 阶段提前问门槛：静默累计超过该值即开始先行咨询 model 插槽
  /// （远程模型自带节流去重，逐帧透传 null 无谓开销）。零（默认）=关闭
  /// 提前问，保持预注册插槽语义——只在静默档阈值帧随插槽链咨询。
  Duration modelEarlyQueryAfter;

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
    this.modelEarlyQueryAfter = Duration.zero,
    this.ruleStage,
  });

  bool get speechConfirmed => _confirmed > 0;
  bool get decided => _decided;

  /// 插槽否决是否仍在压制静默档（否决后尚未再累计满一段生效阈值）。
  /// 静默兜底定时器触发时若否决仍在，应重新武装定时器而非收口
  /// （voice-turn-detection 决策 c：模型认为没说完，能量线不得抢判）。
  bool get vetoActive =>
      _vetoedAtSilenceMs >= 0 &&
      _silenceRunFrames * turnFrameMs - _vetoedAtSilenceMs <
          effectiveSilenceThreshold.inMilliseconds;

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
    // model 阶段提前问（voice-turn-detection 决策 c 接线）：静默累计超过
    // modelEarlyQueryAfter 即先行咨询，true 提前判完、false 走既有否决
    // 语义、null 透传。门槛为零（默认）时保持预注册语义——只在阈值帧咨询。
    final earlyQueryMs = modelEarlyQueryAfter.inMilliseconds;
    if (modelStage != null &&
        (earlyQueryMs > 0
            ? silenceMs >= earlyQueryMs
            : silenceMs >= threshold.inMilliseconds)) {
      final verdict = modelStage!(context);
      if (verdict != null) {
        if (verdict) {
          return _fire(TurnDetectionStage.model, threshold);
        }
        _vetoedAtSilenceMs = silenceMs;
        return TurnObservation(
          stage: TurnDetectionStage.model,
          decided: false,
          speechConfirmed: true,
          effectiveSilenceThreshold: threshold,
        );
      }
    }
    if (silenceMs < threshold.inMilliseconds) {
      return TurnObservation(
        stage: TurnDetectionStage.silence,
        decided: false,
        speechConfirmed: true,
        effectiveSilenceThreshold: threshold,
      );
    }
    // 阈值帧：规则插槽（model 本帧已问过或未接入）→ 静默档兜底。
    final rule = ruleStage;
    if (rule != null) {
      final verdict = rule(context);
      if (verdict != null) {
        if (verdict) {
          return _fire(TurnDetectionStage.completenessRule, threshold);
        }
        _vetoedAtSilenceMs = silenceMs;
        return TurnObservation(
          stage: TurnDetectionStage.completenessRule,
          decided: false,
          speechConfirmed: true,
          effectiveSilenceThreshold: threshold,
        );
      }
    }
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

/// turn_query 上行发送器：走既有 WS（WebSocketService.send），
/// 返回 false 表示连接不可用（消息未入队）。
typedef TurnQuerySender = bool Function(Map<String, dynamic> message);

/// 远程轮次模型插槽（voice-turn-detection 决策 c 的客户端接线端）：
/// 把链上 model 阶段的咨询转成 turn_query 上行，服务端转发 sidecar 后以
/// turn_result 回填（acceptResult），下一次链咨询取走结论。
///
/// 咨询节奏（grilling 已定契约）：
/// - 提前问：链以 modelEarlyQueryAfter=600ms 开启提前咨询；本类内部再
///   守 [queryStartAfter] 门槛、[resendThrottle] 节流与单飞（同一话轮
///   同时至多一条在途查询）；
/// - 文本源：最近一次 transcript partial（noteText 注入）；无文本不判，
///   null 下探静默档；
/// - 结论消费：结论绑定其判定时的文本，文本不变期间持续有效——链每次
///   咨询都取到同一结论（false 反复否决不重发请求，true 判完后话轮收口）；
///   partial 文本更新即作废（重询带新文本）；utteranceId 关联，错话轮的
///   迟到结果丢弃；
/// - 挂起保险：同文本连续否决达到 [maxFalseVerdicts] 后本话轮放弃远程
///   判定（null 下探静默档）——防止模型误判把话轮无限挂起；
/// - 降级：isComplete=null（服务端未配置/失败）与发送失败在话轮内粘滞
///   （null 不重询，链稳定下探静默档）；在途超时只按未决处理（可再询）。
/// 全部降级路径都不产生客户端特殊分支——插槽 null 语义即降级链本身。
class RemoteTurnModel {
  RemoteTurnModel({
    required this.send,
    this.queryStartAfter = const Duration(milliseconds: turnModelEarlyQueryMs),
    this.resendThrottle = const Duration(milliseconds: 300),
    this.queryTimeout = const Duration(milliseconds: 450),
    this.maxFalseVerdicts = 2,
  });

  final TurnQuerySender send;

  /// 起查门槛：静默累计低于该值不询（与链的提前问门槛同值，双保险）。
  final Duration queryStartAfter;

  /// 节流：两次查询的最小间隔。
  final Duration resendThrottle;

  /// 在途超时：超过即按未决（可再询），与链阈值帧前的时间预算匹配。
  final Duration queryTimeout;

  /// 同文本连续否决上限：达到后本话轮放弃远程判定（防误判无限挂起）。
  /// 2 次否决在默认 1400ms 档覆盖约 3.4s 的思考停顿（600ms 起查 + 两段
  /// 生效阈值），足以覆盖评估 A 类的 600-1200ms 停顿分布。
  final int maxFalseVerdicts;

  final Stopwatch _clock = Stopwatch()..start();
  Timer? _timeoutTimer;
  String? _utteranceId;
  String _text = '';
  bool? _verdict;
  // 在途查询对应的文本快照：迟到 turn_result 与当前文本不一致即丢弃。
  String? _inFlightQueryText;
  int _falseRepeats = 0;
  bool _queryInFlight = false;
  bool _unavailable = false;
  int? _lastQueryAtMs;

  /// 链上 model 插槽的咨询入口（TurnStageDelegate 形状）。
  bool? call(TurnDecisionContext context) {
    if (_utteranceId == null || _unavailable) return null;
    final verdict = _verdict;
    if (verdict != null) {
      // 结论绑定其判定时的文本：文本不变期间重复咨询取同一结论（否决
      // 持续有效，不重发请求），文本更新由 noteText 作废。
      if (!verdict) {
        _falseRepeats++;
        if (_falseRepeats > maxFalseVerdicts) {
          // 连续否决超限：本话轮放弃远程判定，交回静默档。
          _verdict = null;
          _unavailable = true;
          return null;
        }
      }
      return verdict;
    }
    if (_queryInFlight) return null;
    if (context.trailingSilence < queryStartAfter) return null;
    final nowMs = _clock.elapsedMilliseconds;
    final lastQueryAtMs = _lastQueryAtMs;
    if (lastQueryAtMs != null &&
        nowMs - lastQueryAtMs < resendThrottle.inMilliseconds) {
      return null;
    }
    final text = _text.trim();
    if (text.isEmpty) return null;
    final sent = send({
      'type': 'turn_query',
      'utteranceId': _utteranceId,
      'text': text,
    });
    _lastQueryAtMs = nowMs;
    if (!sent) {
      // WS 不可达：本话轮放弃远程判定（音频同样没在流），null 粘滞。
      _unavailable = true;
      return null;
    }
    // 记录在途查询的文本代次：partial 更新后迟到的旧文本结论必须在
    // acceptResult 处作废（结论绑定判定时文本的契约对在途竞态同样成立）。
    _inFlightQueryText = text;
    _queryInFlight = true;
    _timeoutTimer?.cancel();
    _timeoutTimer = Timer(queryTimeout, _resolveQueryTimeout);
    return null;
  }

  /// turn_result 下行回填：utteranceId 不匹配的迟到结果丢弃；查询发出后
  /// 文本已被 partial 更新的迟到结论一并丢弃（按旧文本提前截断新话轮）；
  /// isComplete=null 视为模型路不可用（话轮内粘滞）。
  void acceptResult({required String utteranceId, required bool? isComplete}) {
    if (utteranceId.isEmpty || utteranceId != _utteranceId) return;
    _timeoutTimer?.cancel();
    _timeoutTimer = null;
    _queryInFlight = false;
    if (isComplete == null) {
      _unavailable = true;
      return;
    }
    if (_inFlightQueryText != null && _inFlightQueryText != _text.trim()) {
      // 旧文本的结论：丢弃，交回链上下一次咨询（按新文本重询）。
      _inFlightQueryText = null;
      return;
    }
    _inFlightQueryText = null;
    _verdict = isComplete;
  }

  /// 最近一次 transcript partial 注入；文本变化即作废未消费的结论，
  /// 并清零连续否决计数（新文本重新获得完整的否决额度）。
  void noteText({required String utteranceId, required String text}) {
    if (utteranceId.isEmpty || utteranceId != _utteranceId) return;
    if (text != _text) {
      _verdict = null;
      _falseRepeats = 0;
      _text = text;
    }
  }

  /// 绑定新话轮（asr_start 时刻）：全部在途状态随之清零。
  void beginUtterance(String utteranceId) {
    _timeoutTimer?.cancel();
    _timeoutTimer = null;
    _utteranceId = utteranceId;
    _text = '';
    _verdict = null;
    _falseRepeats = 0;
    _queryInFlight = false;
    _unavailable = false;
    _lastQueryAtMs = null;
  }

  void _resolveQueryTimeout() {
    // 超时未回：只按未决处理（可再询），不粘滞——可能是瞬时拥塞，
    // 服务端恢复后下一个节流窗自动续上。
    _queryInFlight = false;
  }

  void dispose() {
    _timeoutTimer?.cancel();
    _timeoutTimer = null;
  }
}
