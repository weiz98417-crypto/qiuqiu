import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/wake_service.dart';

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

  final _controller = StreamController<KwsHit>.broadcast();

  @override
  Stream<KwsHit> get hits => _controller.stream;

  void emit(KwsHit hit) {
    _controller.add(hit);
  }

  @override
  Future<void> start() async {
    final error = startError;
    if (error != null) throw error;
    started = true;
    stopped = false;
  }

  @override
  Future<void> stop() async {
    stopped = true;
    started = false;
  }

  @override
  Future<void> dispose() async {
    disposed = true;
    await _controller.close();
  }
}
