import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:web_socket_channel/web_socket_channel.dart';

class WebSocketService {
  WebSocketChannel? _channel;
  String? _url;
  final _controller = StreamController<Map<String, dynamic>>.broadcast();
  final _binaryController = StreamController<Uint8List>.broadcast();
  Timer? _pingTimer;
  int _reconnectAttempts = 0;
  static const int _maxReconnect = 5;

  Stream<Map<String, dynamic>> get onMessage => _controller.stream;
  Stream<Uint8List> get onBinary => _binaryController.stream;

  void connect(String url) {
    _url = url;
    _doConnect(url);
  }

  void _doConnect(String url) {
    try {
      final uri = Uri.parse(url);
      _channel = WebSocketChannel.connect(uri);

      _channel!.stream.listen(
        (data) {
          _reconnectAttempts = 0;
          if (data is String) {
            try {
              final msg = jsonDecode(data) as Map<String, dynamic>;
              _controller.add(msg);
            } catch (_) {}
          } else if (data is List<int>) {
            _binaryController.add(Uint8List.fromList(data));
          }
        },
        onError: (_) => _tryReconnect(),
        onDone: () => _tryReconnect(),
      );

      _pingTimer?.cancel();
      _pingTimer = Timer.periodic(const Duration(seconds: 25), (_) {
        _channel?.sink.add(jsonEncode({'type': 'ping'}));
      });
    } catch (e) {
      _tryReconnect();
    }
  }

  void _tryReconnect() {
    if (_reconnectAttempts >= _maxReconnect || _url == null) return;
    _reconnectAttempts++;
    final delay = Duration(seconds: (1 << (_reconnectAttempts - 1)).clamp(1, 8));
    _controller.add({'type': 'reconnecting', 'attempt': _reconnectAttempts});
    Future.delayed(delay, () => _doConnect(_url!));
  }

  void send(Map<String, dynamic> msg) {
    _channel?.sink.add(jsonEncode(msg));
  }

  void disconnect() {
    _pingTimer?.cancel();
    _channel?.sink.close();
    _controller.close();
    _binaryController.close();
  }
}
