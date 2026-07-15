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
}
