import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/services.dart' show rootBundle;

/// In-app loopback server for the Live2D webview's asset requests.
///
/// Android WebView cannot fetch() non-http custom schemes — the qiuqiu://asset
/// route broke every PIXI/model/presentation-map load (c60049f), and the
/// interim fix pinned a dev static server to the emulator loopback 10.0.2.2,
/// which does not exist on a release device. This server serves
/// assets/live2d/ from the bundled rootBundle over 127.0.0.1, so the webview
/// fetches real HTTP from an origin the release build carries with it
/// (loopback is also a secure context, which AudioWorklet requires).
class Live2dAssetServer {
  Live2dAssetServer._();

  static final Live2dAssetServer instance = Live2dAssetServer._();

  static const String _routePrefix = '/live2d-assets/';

  static const Map<String, String> _mimeMap = {
    'json': 'application/json',
    'moc3': 'application/octet-stream',
    'png': 'image/png',
    'cdi3': 'application/json',
    'exp3': 'application/json',
    'js': 'application/javascript',
    'mjs': 'application/javascript',
    'bin': 'application/octet-stream',
    'wasm': 'application/wasm',
  };

  Future<String>? _starting;  /// Origin of the server once started, e.g. `http://127.0.0.1:41734`.
  /// Asset URLs are `<origin>/live2d-assets/<path under assets/live2d/>`.
  /// Concurrent callers share one bind; errors propagate to all of them.
  Future<String> start() {
    return _starting ??= _bind();
  }

  Future<String> _bind() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen(_serve, onError: (_) {});
    return 'http://127.0.0.1:${server.port}';
  }

  Future<void> _serve(HttpRequest request) async {
    final response = request.response;
    try {
      final path = request.uri.path;
      if (!path.startsWith(_routePrefix) || path.contains('..')) {
        await _respondStatus(response, HttpStatus.notFound);
        return;
      }
      final assetPath = path.substring(_routePrefix.length);
      if (assetPath.isEmpty) {
        await _respondStatus(response, HttpStatus.notFound);
        return;
      }
      final ByteData data;
      try {
        data = await rootBundle.load('assets/live2d/$assetPath');
      } catch (_) {
        await _respondStatus(response, HttpStatus.notFound);
        return;
      }
      final ext = assetPath.split('.').last.toLowerCase();
      response.statusCode = HttpStatus.ok;
      response.headers.set(
        HttpHeaders.contentTypeHeader,
        _mimeMap[ext] ?? 'application/octet-stream',
      );
      response.headers.set(HttpHeaders.cacheControlHeader, 'no-cache');
      response.add(data.buffer.asUint8List());
      await response.close();
    } catch (_) {
      try {
        await response.close();
      } catch (_) {}
    }
  }

  Future<void> _respondStatus(dynamic response, int status) async {
    response.statusCode = status;
    await response.close();
  }
}
