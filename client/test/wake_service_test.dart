import 'dart:async';
import 'dart:ffi' as ffi;
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/wake_engine_sherpa.dart';
import 'package:qiuqiu/services/wake_service.dart';
import 'package:sherpa_onnx/sherpa_onnx.dart' as sherpa;

/// wake-word-kws 10.2：唤醒状态机用例（KWS 引擎以假实现注入——
/// sherpa 原生库在测试环境不可用，接口替身按 wake_service_test 纪律）。
void main() {
  group('WakeService 状态机', () {
    late _FakeEngine engine;
    late List<KwsHit> emitted;
    late DateTime current;

    setUp(() {
      engine = _FakeEngine();
      emitted = [];
      current = DateTime(2026, 9, 30, 12);
    });

    WakeService build() {
      final service = WakeService(
        engineFactory: (config) async => engine,
        now: () => current,
      );
      service.onWake = (hit) => emitted.add(hit);
      return service;
    }

    test('开启后 armed，命中进 awakening 并回调 onWake', () async {
      final service = build();
      await service.setEnabled(true);
      expect(service.phase, WakePhase.armed);
      expect(engine.started, isTrue);

      engine.emit(hit());
      await pump();
      expect(service.phase, WakePhase.awakening);
      expect(emitted, hasLength(1));
      expect(emitted.single.keyword, '你好球球');
      await service.dispose();
    });

    test('冷却窗内二次命中只唤醒一次，过窗后可再唤醒', () async {
      final service = build();
      await service.setEnabled(true);
      engine.emit(hit());
      await pump();
      // cooldown(1.5s) 内：抑制。
      current = current.add(const Duration(milliseconds: 800));
      engine.emit(hit());
      await pump();
      expect(emitted, hasLength(1));
      // 会话接手再结束：回 armed，过冷却窗后可再次唤醒。
      await service.notifySessionStarted();
      await service.notifySessionEnded();
      current = current.add(const Duration(seconds: 2));
      engine.emit(hit());
      await pump();
      expect(emitted, hasLength(2));
      await service.dispose();
    });

    test('会话开始挂起监听，结束恢复 armed；挂起期命中忽略', () async {
      final service = build();
      await service.setEnabled(true);
      engine.emit(hit());
      await pump();
      await service.notifySessionStarted();
      expect(service.phase, WakePhase.suspended);
      expect(engine.stopped, isTrue);

      engine.emit(hit());
      await pump();
      expect(emitted, hasLength(1), reason: '挂起期命中不得触发唤醒');

      await service.notifySessionEnded();
      expect(service.phase, WakePhase.armed);
      expect(engine.started, isTrue);
      await service.dispose();
    });

    test('用户手动开麦（armed 直达会话）同样挂起', () async {
      final service = build();
      await service.setEnabled(true);
      await service.notifySessionStarted();
      expect(service.phase, WakePhase.suspended);
      await service.dispose();
    });

    test('start() 在途期间会话开启：落地后复查即挂起，不双持麦', () async {
      engine.startGate = Completer<void>();
      final service = build();
      final enabling = service.setEnabled(true);
      await pump(); // start() 挂在闸门上，落地检查点已过。
      await service.notifySessionStarted(); // 会话开启：_pendingSuspend 置位。
      engine.startGate!.complete();
      await enabling;
      expect(service.phase, WakePhase.suspended,
          reason: '落地后复查避让旗标，KWS 不与活跃 VAD 双持麦克风');
      expect(engine.stopped, isTrue);
      await service.dispose();
    });

    test('start 在途期间会话快速交替：迟到落地不得回 armed 双持麦', () async {
      final service = build();
      await service.setEnabled(true);
      await service.notifySessionStarted(); // suspended（stop 已落地）。
      engine.startGate = Completer<void>();
      final ending = service.notifySessionEnded(); // start 在途，phase 仍 suspended。
      await pump();
      await service.notifySessionStarted(); // 新会话接手：_sessionActive=true，
      // phase==suspended 走空分支不会停收音——迟到 start 落地必须自查让麦。
      engine.startGate!.complete();
      await ending;
      expect(service.phase, WakePhase.suspended,
          reason: '会话再次活跃：迟到 start 落地不得把 KWS 送回 armed 抢麦');
      expect(engine.started, isFalse, reason: '活跃会话期间 KWS 不得持收音');
      await service.dispose();
    });

    test('disable 穿插 stop 在途：迟到挂起不得覆写关机收敛（卡死 suspended）',
        () async {
      final service = build();
      await service.setEnabled(true);
      engine.stopGate = Completer<void>();
      final suspending = service.notifySessionStarted(); // stop 在途。
      await pump();
      await service.setEnabled(false); // disabled、引擎已释放。
      engine.stopGate!.complete();
      await suspending;
      expect(service.phase, WakePhase.disabled,
          reason: '关机收敛不被在途 stop 的迟到 suspended 覆写');
      // 卡死复验：若迟到挂起覆写成 suspended 且引擎已缺失，setEnabled(true)
      // 会被「非 disabled/failure 不落地」守卫永久拒收。重新落地必须成功，
      // 且状态机对会话事件仍响应（会话旗标按设计跨开关存活：先互斥挂起，
      // 会话结束即恢复监听）。
      engine = _FakeEngine();
      await service.setEnabled(true);
      expect(service.phase, WakePhase.suspended,
          reason: '会话旗标未清：重试落地照互斥挂起');
      await service.notifySessionEnded();
      expect(service.phase, WakePhase.armed, reason: '关机循环不留下死态');
      await service.dispose();
    });

    test('started→ended 均落在 stop 在途窗内：不得卡 suspended', () async {
      final service = build();
      await service.setEnabled(true);
      engine.stopGate = Completer<void>();
      final suspending = service.notifySessionStarted(); // stop 在途。
      await pump();
      await service.notifySessionEnded(); // phase 仍 armed：早退只清旗标。
      engine.stopGate!.complete();
      await suspending;
      expect(service.phase, WakePhase.armed,
          reason: '会话未及接手就结束：恢复空闲监听而不是永久挂起');
      expect(engine.started, isTrue);
      await service.dispose();
    });

    test('disable 穿插 start 在途：迟到失败不得把 disabled 覆写成 failure',
        () async {
      engine.startGate = Completer<void>();
      engine.startError = StateError('late boom');
      final service = build();
      final enabling = service.setEnabled(true);
      await pump(); // start 挂闸。
      await service.setEnabled(false);
      engine.startGate!.complete();
      await enabling;
      expect(service.phase, WakePhase.disabled,
          reason: '关机收敛不被迟到失败覆写');
      expect(service.failureReason, isNull);
      await service.dispose();
    });

    test('disable 穿插工厂在途：工厂迟到抛错不得把 disabled 覆写成 failure',
        () async {
      final gate = Completer<void>();
      final service = WakeService(
        engineFactory: (config) async {
          await gate.future;
          throw StateError('late factory boom');
        },
        now: () => current,
      );
      final enabling = service.setEnabled(true);
      await pump();
      await service.setEnabled(false);
      gate.complete();
      await enabling;
      expect(service.phase, WakePhase.disabled,
          reason: '关机收敛不被迟到工厂失败覆写');
      expect(service.failureReason, isNull);
      await service.dispose();
    });

    test('唤醒无人接手：openTimeout 后回 armed，不自动二次唤醒', () async {
      final service = WakeService(
        engineFactory: (config) async => engine,
        config: const WakeConfig(openTimeout: Duration(milliseconds: 10)),
        now: () => current,
      );
      service.onWake = (wake) => emitted.add(wake);
      await service.setEnabled(true);
      engine.emit(hit());
      await pump();
      expect(service.phase, WakePhase.awakening);

      await Future<void>.delayed(const Duration(milliseconds: 30));
      expect(service.phase, WakePhase.armed);
      expect(emitted, hasLength(1), reason: '超时回静默不重复回调');
      await service.dispose();
    }, timeout: _shortTimeout);

    test('awakening 态用户开口（notifyUserSpoke）解除超时回退', () async {
      final service = build();
      await service.setEnabled(true);
      engine.emit(hit());
      await pump();
      service.notifyUserSpoke();
      // openTimeout(默认 8s) 远大于用例观察窗：这里用相位守恒验证——
      // awakening 保持（超时已解除），直到会话接手。
      expect(service.phase, WakePhase.awakening);
      await service.notifySessionStarted();
      expect(service.phase, WakePhase.suspended);
      await service.dispose();
    }, timeout: _shortTimeout);

    test('starting 期间会话已开：落地即 suspended（pendingSuspend）', () async {
      final gate = Completer<void>();
      final slowService = WakeService(
        engineFactory: (config) async {
          await gate.future;
          return engine;
        },
        now: () => current,
      );
      final enabling = slowService.setEnabled(true);
      await slowService.notifySessionStarted();
      gate.complete();
      await enabling;
      expect(slowService.phase, WakePhase.suspended);
      expect(engine.started, isFalse, reason: '挂起落地不得抢麦克风');
      await slowService.dispose();
    });

    test('引擎工厂返回 null → failure 且带原因；关开可重试', () async {
      final service = WakeService(
        engineFactory: (config) async => null,
        now: () => current,
      );
      await service.setEnabled(true);
      expect(service.phase, WakePhase.failure);
      expect(service.failureReason, isNotNull);

      // 换可用引擎（模拟平台恢复/模型补齐）后重开。
      var attempt = 0;
      final retryService = WakeService(
        engineFactory: (config) async => attempt++ == 0 ? null : engine,
        now: () => current,
      );
      await retryService.setEnabled(true);
      expect(retryService.phase, WakePhase.failure);
      await retryService.setEnabled(false);
      expect(retryService.phase, WakePhase.disabled);
      await retryService.setEnabled(true);
      expect(retryService.phase, WakePhase.armed);
      await retryService.dispose();
    });

    test('引擎 start 抛错 → failure，不回调唤醒', () async {
      final service = WakeService(
        engineFactory: (config) async => engine..startError = StateError('boom'),
        now: () => current,
      );
      service.onWake = (hit) => emitted.add(hit);
      await service.setEnabled(true);
      expect(service.phase, WakePhase.failure);
      engine.emit(hit());
      await pump();
      expect(emitted, isEmpty);
      await service.dispose();
    });

    test('failure 期间会话已开：开关循环重试落地即 suspended，不双持麦',
        () async {
      // 先制造 failure（模型缺失类），期间 VAD 会话已开（麦克风归会话）。
      var attempt = 0;
      final service = WakeService(
        engineFactory: (config) async => attempt++ == 0 ? null : engine,
        now: () => current,
      );
      await service.setEnabled(true);
      expect(service.phase, WakePhase.failure);
      await service.notifySessionStarted();
      expect(service.phase, WakePhase.failure);

      // 关开一次重试：落地前先确认会话态——活跃则进 suspended 而非 armed。
      await service.setEnabled(false);
      await service.setEnabled(true);
      expect(service.phase, WakePhase.suspended);
      expect(engine.started, isFalse, reason: '会话活跃：重试落地不得让 KWS 抢收音');

      // 会话结束才恢复空闲监听。
      await service.notifySessionEnded();
      expect(service.phase, WakePhase.armed);
      expect(engine.started, isTrue);
      await service.dispose();
    });

    test('关闭：任意态收敛 disabled 并释放引擎', () async {
      final service = build();
      await service.setEnabled(true);
      engine.emit(hit());
      await pump();
      await service.setEnabled(false);
      expect(service.phase, WakePhase.disabled);
      expect(engine.disposed, isTrue);
      await service.dispose();
    });

    test('dispose 后开启请求被忽略', () async {
      final service = build();
      await service.dispose();
      await service.setEnabled(true);
      expect(service.phase, WakePhase.disabled);
    });
  });

  group('SherpaKwsEngine stop 契约', () {
    // 原生库在测试环境不可用：stream/spotter 以 nullptr 桩构造（纯 Dart
    // 赋值构造器，不触原生），解码排水经 isReady=false 短路——只观察
    // acceptWaveform 是否被在途回调触碰。
    test('stop 后在途 pcm 回调被忽略（use-after-free 封口）', () async {
      final engine = SherpaKwsEngine(config: const WakeConfig());
      final stream = _RecordingStream();
      final spotter = _StubSpotter();

      // stop 前：在途块照常受理（生产里此刻 stream 尚未释放）。
      engine.processPcmForTest(_pcmChunk(), stream, spotter);
      expect(stream.acceptCalls, 1);

      // stop：置停歇旗标并释放引擎持有的 stream（未 start，此处为空操作）。
      await engine.stop();

      // stop 后送达的已入队块：见停歇旗标即返——不 acceptWaveform、
      // 不解码、不触碰已 free 的原生 stream。
      engine.processPcmForTest(_pcmChunk(), stream, spotter);
      expect(stream.acceptCalls, 1, reason: 'stop 后在途回调必须被忽略');

      await engine.dispose();
    });
  });
}

/// 100ms 级 pcm16 块替身：非空即可走进 acceptWaveform。
Uint8List _pcmChunk() =>
    Uint8List.fromList(List<int>.generate(640, (i) => i & 0xff));

/// nullptr 桩 stream：构造只赋值不触原生；acceptWaveform/free 记账不调
/// super（bindings 未初始化时 super 会抛）。
class _RecordingStream extends sherpa.OnlineStream {
  int acceptCalls = 0;
  int freedCount = 0;

  _RecordingStream() : super(ptr: ffi.nullptr);

  @override
  void acceptWaveform({required Float32List samples, required int sampleRate}) {
    acceptCalls++;
  }

  @override
  void free() {
    freedCount++;
  }
}

/// nullptr 桩 spotter：isReady 恒假，跳过解码排水（原生不可用）。
class _StubSpotter extends sherpa.KeywordSpotter {
  _StubSpotter()
      : super.fromPtr(
          ptr: ffi.nullptr,
          config: const sherpa.KeywordSpotterConfig(
            model: sherpa.OnlineModelConfig(tokens: 'test'),
          ),
        );

  @override
  bool isReady(sherpa.OnlineStream stream) => false;
}

/// 固定命中时刻：服务用注入的 now() 判冷却，KwsHit.at 仅作载体。
final _frozen = DateTime(2026, 9, 30, 12);

KwsHit hit({String keyword = '你好球球'}) =>
    KwsHit(keyword: keyword, at: _frozen);

/// 广播流投递是异步的：命中 emit 后先排水事件循环再断言。
Future<void> pump() => Future<void>.delayed(Duration.zero);

const _shortTimeout = Timeout(Duration(seconds: 5));

class _FakeEngine implements KwsEngine {
  bool started = false;
  bool stopped = false;
  bool disposed = false;
  Object? startError;

  /// 非空时 start() 挂起至此门释放——复现「落地越过互斥检查点」的在途竞态。
  Completer<void>? startGate;

  /// 非空时 stop() 挂起至此门释放——复现 stop 在途窗内的穿插竞态。
  Completer<void>? stopGate;

  final _controller = StreamController<KwsHit>.broadcast();

  @override
  Stream<KwsHit> get hits => _controller.stream;

  void emit(KwsHit hit) {
    _controller.add(hit);
  }

  @override
  Future<void> start() async {
    final gate = startGate;
    if (gate != null) {
      await gate.future;
    }
    final error = startError;
    if (error != null) throw error;
    started = true;
    stopped = false;
  }

  @override
  Future<void> stop() async {
    final gate = stopGate;
    if (gate != null) {
      await gate.future;
    }
    stopped = true;
    started = false;
  }

  @override
  Future<void> dispose() async {
    disposed = true;
    await _controller.close();
  }
}
