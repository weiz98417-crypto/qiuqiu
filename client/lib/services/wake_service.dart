import 'dart:async';

/// 唤醒词状态机（wake-word-kws 10.2）：空闲监听 ⇄ 语音会话互斥。
///
/// 本文件不含 sherpa_onnx 依赖——KWS 引擎经 [KwsEngine] 接口注入
/// （平台实现在 wake_engine_sherpa.dart，web/测试用替身），状态机可在
/// `flutter test` 里以假引擎驱动（wake_service_test.dart）。
///
/// 范围=前台空闲态：仅在比赛页打开、语音会话（VAD 监听）未激活时 armed；
/// 唤醒→觉醒态（等会话接手）→会话挂起 KWS；会话结束回 armed。VAD 不跑、
/// 音频不落盘，引擎持有麦克风与 VAD 互斥（谁在听谁持有，见 match_screen 接线）。
///
/// 词表/参数（wake-word-kws 评估门定稿，scripts/wake-eval/results/
/// FINAL-nihao-s1.8-t0.3.json）：「你好球球」，score=1.8 / threshold=0.3 /
/// cooldown=1.5s。换词只改 assets/wake/keywords.txt，不改代码。

enum WakePhase {
  /// 开关关（默认）：无引擎、无监听。
  disabled,

  /// 引擎加载中（模型拷贝+实例化），一次性过渡态。
  starting,

  /// 空闲监听中：命中即唤醒。
  armed,

  /// 已唤醒、等待会话接手（VAD startListening）；超过 openTimeout 无人接手
  /// 则自动回 [armed]，不再自动二次唤醒。
  awakening,

  /// 语音会话进行中：KWS 挂起（麦克风归 VAD）。
  suspended,

  /// 引擎初始化失败（平台不支持/模型缺失/加载异常）：开关仍算开，但本次
  /// 生命周期内不再重试；关开一次可重试。
  failure,
}

/// 一次 KWS 命中。
class KwsHit {
  final String keyword;
  final DateTime at;

  const KwsHit({required this.keyword, required this.at});
}

/// KWS 引擎接口：实现方负责模型加载、收音、解码；命中经 [hits] 上抛。
/// start/stop 只切换收音（可重复调用），dispose 释放全部资源。
abstract class KwsEngine {
  Stream<KwsHit> get hits;

  Future<void> start();

  Future<void> stop();

  Future<void> dispose();
}

/// 引擎工厂：返回 null 或抛错都归入 failure 态（平台不支持/模型缺失）。
typedef KwsEngineFactory = Future<KwsEngine?> Function(WakeConfig config);

class WakeConfig {
  /// 评估定稿起步参数（见文件头注释），实测可调。
  final double keywordsScore;
  final double keywordsThreshold;

  /// 命中抑制窗：窗内二次命中不算新唤醒。
  final Duration cooldown;

  /// 唤醒后会话未接手（VAD 没起来）或接手后无语音的回静默时限。
  final Duration openTimeout;

  const WakeConfig({
    this.keywordsScore = 1.8,
    this.keywordsThreshold = 0.3,
    this.cooldown = const Duration(milliseconds: 1500),
    this.openTimeout = const Duration(seconds: 8),
  });
}

class WakeService {
  WakeService({
    required KwsEngineFactory engineFactory,
    this.config = const WakeConfig(),
    DateTime Function()? now,
  })  : _engineFactory = engineFactory,
        _now = now ?? DateTime.now;

  final KwsEngineFactory _engineFactory;
  final WakeConfig config;
  final DateTime Function() _now;

  WakePhase _phase = WakePhase.disabled;
  KwsEngine? _engine;
  StreamSubscription<KwsHit>? _hitsSub;
  Timer? _openTimer;
  DateTime _lastWakeAt = DateTime.fromMillisecondsSinceEpoch(0);

  /// starting 未落地时的挂起/关闭请求落地旗标（测试用例覆盖）。
  bool _pendingSuspend = false;
  bool _disposed = false;
  String? _failureReason;

  /// 唤醒回调：armed 态命中且过冷却窗时触发（每唤醒一次一次）。
  void Function(KwsHit hit)? onWake;

  WakePhase get phase => _phase;
  String? get failureReason => _failureReason;
  KwsEngine? get engineForTest => _engine;

  final _phaseChanges = StreamController<WakePhase>.broadcast();
  Stream<WakePhase> get phaseChanges => _phaseChanges.stream;

  /// 用户开关（设置页，默认关）。关：任意态收敛到 disabled 并释放引擎。
  Future<void> setEnabled(bool enabled) async {
    if (!enabled) {
      _openTimer?.cancel();
      _pendingSuspend = false;
      _failureReason = null;
      final engine = _engine;
      _engine = null;
      _hitsSub?.cancel();
      _hitsSub = null;
      if (engine != null) {
        try {
          await engine.dispose();
        } catch (_) {
          // 释放失败不阻断状态收敛。
        }
      }
      _setPhase(WakePhase.disabled);
      return;
    }
    if (_phase != WakePhase.disabled && _phase != WakePhase.failure) return;
    if (_disposed) return;
    _failureReason = null;
    _setPhase(WakePhase.starting);
    final KwsEngine? engine;
    try {
      engine = await _engineFactory(config);
      if (engine == null) {
        throw StateError('wake engine unavailable on this platform');
      }
    } catch (error) {
      _failureReason = error.toString();
      _setPhase(WakePhase.failure);
      return;
    }
    if (_disposed || _phase != WakePhase.starting) {
      // starting 期间被关掉：资源立即回收。
      try {
        await engine.dispose();
      } catch (_) {}
      return;
    }
    _engine = engine;
    _hitsSub = engine.hits.listen(_onHit);
    if (_pendingSuspend) {
      // starting 期间会话已开：落地即挂起（麦克风让给 VAD），不起收音。
      _pendingSuspend = false;
      try {
        await engine.stop();
        _setPhase(WakePhase.suspended);
      } catch (error) {
        _failureReason = error.toString();
        _setPhase(WakePhase.failure);
      }
      return;
    }
    try {
      await engine.start();
    } catch (error) {
      _failureReason = error.toString();
      _setPhase(WakePhase.failure);
      return;
    }
    _setPhase(WakePhase.armed);
  }

  /// 语音会话开始（VAD listening）：互斥挂起。armed/awakening/starting 均可
  /// 进入（用户手动开麦不等唤醒）。
  Future<void> notifySessionStarted() async {
    _openTimer?.cancel();
    switch (_phase) {
      case WakePhase.armed:
      case WakePhase.awakening:
        await _engine?.stop();
        _setPhase(WakePhase.suspended);
        break;
      case WakePhase.starting:
        _pendingSuspend = true;
        break;
      case WakePhase.disabled:
      case WakePhase.suspended:
      case WakePhase.failure:
        break;
    }
  }

  /// 语音会话结束（VAD idle/停麦）：恢复空闲监听。
  Future<void> notifySessionEnded() async {
    if (_phase != WakePhase.suspended) return;
    final engine = _engine;
    if (engine == null) return;
    try {
      await engine.start();
      _setPhase(WakePhase.armed);
    } catch (error) {
      _failureReason = error.toString();
      _setPhase(WakePhase.failure);
    }
  }

  /// 唤醒打开的会话里用户开口了：解除「无后续语音回静默」超时。
  /// 会话由 VAD 接管至自然结束（notifySessionEnded 恢复监听）。
  void notifyUserSpoke() {
    if (_phase == WakePhase.awakening) {
      _openTimer?.cancel();
    }
  }

  void _onHit(KwsHit hit) {
    if (_phase != WakePhase.armed) return; // 非空闲态命中一律忽略
    final now = _now();
    if (now.difference(_lastWakeAt) < config.cooldown) return;
    _lastWakeAt = now;
    _setPhase(WakePhase.awakening);
    onWake?.call(hit);
    // 唤醒无人接手：回静默（armed），不再自动二次唤醒。
    _openTimer?.cancel();
    _openTimer = Timer(config.openTimeout, () {
      if (_phase == WakePhase.awakening) {
        _setPhase(WakePhase.armed);
      }
    });
  }

  void _setPhase(WakePhase phase) {
    if (_phase == phase) return;
    _phase = phase;
    if (!_phaseChanges.isClosed) {
      _phaseChanges.add(phase);
    }
  }

  /// 页面退出：释放全部资源，此后 setEnabled(true) 无效。
  Future<void> dispose() async {
    _disposed = true;
    _openTimer?.cancel();
    _hitsSub?.cancel();
    final engine = _engine;
    _engine = null;
    try {
      await engine?.dispose();
    } catch (_) {}
    await _phaseChanges.close();
  }
}
