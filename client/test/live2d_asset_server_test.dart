import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/live2d_asset_server.dart';

/// TestWidgetsFlutterBinding 会拦截所有 HttpClient 请求(统一回 400),
/// 这里全部用裸 socket 发 HTTP,绕开被拦截的 client。
class _RawResponse {
  final int status;
  final Map<String, String> headers;
  final List<int> body;
  _RawResponse(this.status, this.headers, this.body);
}

Future<_RawResponse> _rawRequest(int port, String requestTarget) async {
  final socket = await Socket.connect('127.0.0.1', port);
  final done = Completer<void>();
  final bytes = BytesBuilder();
  late StreamSubscription<void> sub;
  sub = socket.listen((chunk) {
    bytes.add(chunk);
    final soFar = bytes.takeBytes();
    bytes.add(soFar);
    if (soFar.length >= 4) {
      final list = soFar;
      for (var i = 0; i + 3 < list.length; i++) {
        if (list[i] == 13 && list[i + 1] == 10 && list[i + 2] == 13 &&
            list[i + 3] == 10) {
          // 头部结束;再看有没有消息体(content-length 或连接关闭)。
          final head = String.fromCharCodes(list.take(i).toList());
          final contentLength = RegExp(
            r'content-length:\s*(\d+)',
            multiLine: true,
            caseSensitive: false,
          ).firstMatch(head);
          if (contentLength != null) {
            final bodyStart = i + 4;
            final expected = bodyStart + int.parse(contentLength.group(1)!);
            if (list.length >= expected) {
              done.complete();
              sub.cancel();
            }
          }
          break;
        }
      }
    }
  }, onDone: () {
    if (!done.isCompleted) done.complete();
  });
  socket.write(
    'GET $requestTarget HTTP/1.1\r\n'
    'Host: 127.0.0.1\r\n'
    'Connection: close\r\n'
    '\r\n',
  );
  await done.future.timeout(const Duration(seconds: 5));
  await socket.close();
  final raw = bytes.takeBytes();
  final splitAt = _findHeaderEnd(raw);
  final head = String.fromCharCodes(raw.sublist(0, splitAt));
  final body = raw.sublist(splitAt + 4);
  final lines = head.split('\r\n');
  final status = int.parse(lines.first.split(' ')[1]);
  final headers = <String, String>{};
  for (final line in lines.skip(1)) {
    final idx = line.indexOf(':');
    if (idx > 0) {
      headers[line.substring(0, idx).toLowerCase()] =
          line.substring(idx + 1).trim();
    }
  }
  return _RawResponse(status, headers, body);
}

int _findHeaderEnd(List<int> raw) {
  for (var i = 0; i + 3 < raw.length; i++) {
    if (raw[i] == 13 && raw[i + 1] == 10 && raw[i + 2] == 13 && raw[i + 3] == 10) {
      return i;
    }
  }
  throw StateError('no header terminator');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String origin;
  late int port;

  setUpAll(() async {
    origin = await Live2dAssetServer.instance.start();
    port = Uri.parse(origin).port;
  });

  test('binds to loopback only', () {
    expect(origin, startsWith('http://127.0.0.1:'));
  });

  test('serves a bundled live2d asset with the right content type', () async {
    final resp =
        await _rawRequest(port, '/live2d-assets/live2d-display-bundle.js');
    expect(resp.status, HttpStatus.ok);
    expect(resp.headers['content-type'], contains('application/javascript'));
    expect(resp.body, isNotEmpty);
  });

  test('serves a binary asset as octet-stream', () async {
    final resp =
        await _rawRequest(port, '/live2d-assets/vendor/wlipsync/profile.bin');
    expect(resp.status, HttpStatus.ok);
    expect(resp.headers['content-type'], contains('application/octet-stream'));
    expect(resp.body, isNotEmpty);
  });

  test('missing asset is 404', () async {
    final resp = await _rawRequest(port, '/live2d-assets/no/such/file.json');
    expect(resp.status, HttpStatus.notFound);
  });

  test('path outside the route prefix is 404', () async {
    final resp = await _rawRequest(port, '/etc/passwd');
    expect(resp.status, HttpStatus.notFound);
  });

  test('traversal attempt is rejected', () async {
    final resp = await _rawRequest(port, '/live2d-assets/../pubspec.yaml');
    expect(resp.status, HttpStatus.notFound);
  });

  test('start is idempotent', () async {
    final again = await Live2dAssetServer.instance.start();
    expect(again, origin);
  });
}
