import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/websocket_service.dart';

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
}
