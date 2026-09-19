import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/websocket_service.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

void main() {
  test('send returns false after the active socket starts closing', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final accepted = Completer<WebSocket>();
    server.listen((request) async {
      accepted.complete(await WebSocketTransformer.upgrade(request));
    });
    final service = WebSocketService();
    addTearDown(service.dispose);
    addTearDown(() => server.close(force: true));

    final connected = service.statusStream.firstWhere(
      (status) => status == SocketStatus.connected,
    );
    service.connect('ws://127.0.0.1:${server.port}');
    await connected;
    final socket = await accepted.future;

    expect(service.send({'type': 'ping'}), isTrue);
    expect(jsonDecode(await socket.first as String), {'type': 'ping'});

    final reconnecting = service.statusStream.firstWhere(
      (status) => status == SocketStatus.reconnecting,
    );
    await socket.close();
    await reconnecting;
    expect(service.send({'type': 'ping'}), isFalse);
  });

  test('refreshes the access token before reconnecting', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final firstAccepted = Completer<WebSocket>();
    final secondAccepted = Completer<WebSocket>();
    var acceptedCount = 0;
    final acceptedTwice = Completer<void>();
    server.listen((request) async {
      final socket = await WebSocketTransformer.upgrade(request);
      acceptedCount++;
      if (acceptedCount == 1) {
        firstAccepted.complete(socket);
      } else if (acceptedCount == 2) {
        secondAccepted.complete(socket);
        acceptedTwice.complete();
      }
    });
    final service = WebSocketService();
    addTearDown(service.dispose);
    addTearDown(() async {
      if (firstAccepted.isCompleted) {
        await firstAccepted.future.then((socket) => socket.close());
      }
      if (secondAccepted.isCompleted) {
        await secondAccepted.future.then((socket) => socket.close());
      }
      await server.close(force: true);
    });

    var refreshCount = 0;
    service.setRefreshTokenCallback(() async {
      refreshCount++;
      return 'new-access-token';
    });
    final firstConnected = service.statusStream.firstWhere(
      (status) => status == SocketStatus.connected,
    );
    final allConnected = service.statusStream
        .where((status) => status == SocketStatus.connected)
        .take(2)
        .toList();
    service.connect('ws://127.0.0.1:${server.port}', token: 'old-token');
    await firstConnected;
    await (await firstAccepted.future).close();
    await acceptedTwice.future.timeout(const Duration(seconds: 5));
    await allConnected;

    expect(refreshCount, 1);
  });

  test('a broken old channel cannot kill the reconnect flow', () async {
    final broken = _StubChannel(
      onClose: () => Future<void>.error(StateError('socket closed')),
    );
    final healthy = _StubChannel();
    var served = 0;
    final service = WebSocketService(
      connectionFactory: (_, __) => served++ == 0 ? broken : healthy,
    );
    addTearDown(service.dispose);
    final firstConnected = service.statusStream.firstWhere(
      (status) => status == SocketStatus.connected,
    );
    final twiceConnected = service.statusStream
        .where((status) => status == SocketStatus.connected)
        .take(2)
        .toList();

    service.connect('ws://stub/first');
    await firstConnected;
    // 第二次 _open：旧 channel 的 sink.close() 抛错——重连流程必须照常重建。
    service.connect('ws://stub/second');
    await twiceConnected;

    expect(served, 2);
    expect(broken.sink.closeCalls, 1);
  });

  test('send failure schedules a reconnect', () async {
    final service = WebSocketService(
      connectionFactory: (_, __) => _StubChannel(
        onAdd: (_) => throw StateError('broken pipe'),
      ),
    );
    addTearDown(service.dispose);
    final connected = service.statusStream.firstWhere(
      (status) => status == SocketStatus.connected,
    );
    final reconnecting = service.statusStream.firstWhere(
      (status) => status == SocketStatus.reconnecting,
    );

    service.connect('ws://stub/half-open');
    await connected;

    // 半开连接：写失败必须立刻进入可见的重连状态。
    expect(service.send({'type': 'ping'}), isFalse);
    await reconnecting;
  });

  test('speech protocol messages carry the client timezone', () {
    expect(
      withClientContext(
        {'type': 'user_speech', 'text': '明天有什么比赛？'},
        'Asia/Shanghai',
      ),
      {
        'type': 'user_speech',
        'text': '明天有什么比赛？',
        'timezone': 'Asia/Shanghai',
      },
    );
    expect(
      withClientContext(
        {'type': 'asr_start', 'timezone': 'Europe/Berlin'},
        'Asia/Shanghai',
      )['timezone'],
      'Europe/Berlin',
    );
    expect(
      withClientContext({'type': 'ping'}, 'Asia/Shanghai'),
      {'type': 'ping'},
    );
  });
}

class _StubChannel implements SocketConnection {
  _StubChannel({
    void Function(Object message)? onAdd,
    Future<void> Function()? onClose,
  }) : sink = _StubSink(onAdd: onAdd, onClose: onClose);

  @override
  final _StubSink sink;
  final _incoming = StreamController<dynamic>();

  @override
  Future<void> get ready => Future<void>.value();

  @override
  Stream<dynamic> get stream => _incoming.stream;
}

class _StubSink implements WebSocketSink {
  _StubSink({
    void Function(Object message)? onAdd,
    Future<void> Function()? onClose,
  })  : _onAdd = onAdd,
        _onClose = onClose;

  final void Function(Object message)? _onAdd;
  final Future<void> Function()? _onClose;
  final _done = Completer<void>();
  int closeCalls = 0;

  @override
  void add(dynamic data) {
    final hook = _onAdd;
    if (hook != null) hook(data);
  }

  @override
  void addError(Object error, [StackTrace? stackTrace]) {}

  @override
  Future<void> addStream(Stream<dynamic> stream) => Future<void>.value();

  @override
  Future<void> close([int? closeCode, String? closeReason]) {
    closeCalls++;
    final hook = _onClose;
    if (hook != null) return hook();
    if (!_done.isCompleted) _done.complete();
    return _done.future;
  }

  @override
  Future<void> get done => _done.future;
}
