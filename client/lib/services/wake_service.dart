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

  /// 会话活跃（VAD 持麦）真值：notifySessionStarted/Ended 维护。failure 态
  /// 期间的会话不经过 armed，状态机不留痕迹——failure→开关循环重试落地时
  /// 据此避让，不让 KWS 与活跃 VAD 双持麦克风。
  bool _sessionActive = false;

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
      if (_disposed || _phase != WakePhase.starting) {
        // starting 期间被关掉：收敛归 setEnabled(false)，迟到失败不覆写。
        return;
      }
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
    if (_pendingSuspend || _sessionActive) {
      // starting/failure 期间会话已开：落地即挂起（麦克风让给 VAD），
      // 不起收音——重试落地不得绕过互斥去抢活跃 VAD 的麦克风。
      _pendingSuspend = false;
      await _suspendLanded(engine);
      return;
    }
    try {
      await engine.start();
    } catch (error) {
      if (_disposed || _phase != WakePhase.starting) return;
      _failureReason = error.toString();
      _setPhase(WakePhase.failure);
      return;
    }
    // 落地后复查：start() 在途期间会话开启（checkpoint 已过、未及避让）
    // 会留下 _pendingSuspend/_sessionActive——立即停收音让麦，不双持。
    if (_pendingSuspend || _sessionActive) {
      _pendingSuspend = false;
      await _suspendLanded(engine);
      return;
    }
    _setPhase(WakePhase.armed);
  }

  /// 挂起落地（麦克风让给活跃会话）。stop 在途被关机穿插时收敛归
  /// setEnabled(false)，迟到的 suspended 不覆写 disabled（否则
  /// setEnabled(true) 被「非 disabled/failure 不落地」守卫永久拒收）。
  Future<void> _suspendLanded(KwsEngine engine) async {
    try {
      await engine.stop();
    } catch (error) {
      if (_disposed || _phase != WakePhase.starting) return;
      _failureReason = error.toString();
      _setPhase(WakePhase.failure);
      return;
    }
    if (_disposed || _phase != WakePhase.starting) return;
    _setPhase(WakePhase.suspended);
  }

  /// 语音会话开始（VAD listening）：互斥挂起。armed/awakening/starting 均可
  /// 进入（用户手动开麦不等唤醒）；failure 态也登记会话态，供后续开关
  /// 循环重试落地时避让。
  Future<void> notifySessionStarted() async {
    _openTimer?.cancel();
    _sessionActive = true;
    switch (_phase) {
      case WakePhase.armed:
      case WakePhase.awakening:
        await _suspendFromIdle(_engine);
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

  /// armed/awakening → suspended 的挂起路径。stop 在途窗内状态可能被
  /// 穿插（关机收敛 / 会话快速翻面），每次 await 落地都复查后再动相位。
  Future<void> _suspendFromIdle(KwsEngine? engine) async {
    await engine?.stop();
    if (_phase != WakePhase.armed && _phase != WakePhase.awakening) {
      return; // 关机等穿插：收敛归 setEnabled(false)，迟到挂起不覆写。
    }
    if (_sessionActive) {
      _setPhase(WakePhase.suspended);
      return;
    }
    // 会话未及接手就结束（started→ended 均落 stop 在途窗内）：恢复
    // 空闲监听而不是永久挂在 suspended。
    if (engine == null) return;
    try {
      await engine.start();
    } catch (error) {
      if (_phase != WakePhase.armed && _phase != WakePhase.awakening) return;
      _failureReason = error.toString();
      _setPhase(WakePhase.failure);
      return;
    }
    if (_sessionActive) {
      // restart 在途期间会话又接手：立即停收音让麦，不落 armed 双持。
      try {
        await engine.stop();
      } catch (_) {}
      if (_phase == WakePhase.armed || _phase == WakePhase.awakening) {
        _setPhase(WakePhase.suspended);
      }
      return;
    }
    if (_phase == WakePhase.armed || _phase == WakePhase.awakening) {
      _setPhase(WakePhase.armed);
    }
  }

  /// 语音会话结束（VAD idle/停麦）：恢复空闲监听。
  Future<void> notifySessionEnded() async {
    _sessionActive = false;
    if (_phase != WakePhase.suspended) {
      // 会话在落地前已结束（starting/failure 期间开又关）：清掉挂起请求，
      // 引擎落地后回 armed 而不是永久 suspended。
      _pendingSuspend = false;
      return;
    }
    final engine = _engine;
    if (engine == null) return;
    try {
      await engine.start();
    } catch (error) {
      if (_phase != WakePhase.suspended) return; // 在途穿插：收敛归它管。
      _failureReason = error.toString();
      _setPhase(WakePhase.failure);
      return;
    }
    if (_phase != WakePhase.suspended) return; // start 在途被关机穿插。
    if (_sessionActive) {
      // 快速交替：start 在途期间新会话已接手（notifySessionStarted 落在
      // suspended 分支不会停收音）——立即停收音让麦，不落 armed 双持。
      try {
        await engine.stop();
      } catch (_) {}
      return;
    }
    _setPhase(WakePhase.armed);
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
