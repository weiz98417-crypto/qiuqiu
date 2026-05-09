import 'dart:async';
import 'dart:convert';
import 'package:web_socket_channel/web_socket_channel.dart';

class WebSocketService {
  WebSocketChannel? _channel;
  final _controller = StreamController<Map<String, dynamic>>.broadcast();
  Timer? _pingTimer;

  Stream<Map<String, dynamic>> get onMessage => _controller.stream;

  void connect(String url) {
    try {
      final uri = Uri.parse(url);
      _channel = WebSocketChannel.connect(uri);

      _channel!.stream.listen(
        (data) {
          try {
            final msg = jsonDecode(data as String) as Map<String, dynamic>;
            _controller.add(msg);
          } catch (_) {
            // binary frame — ignore for now
          }
        },
        onError: (error) {
          _controller.addError(error);
        },
      );

      // Ping every 25s to keep alive
      _pingTimer = Timer.periodic(const Duration(seconds: 25), (_) {
        _channel?.sink.add(jsonEncode({'type': 'ping'}));
      });
    } catch (e) {
      _controller.addError(e);
    }
  }

  void disconnect() {
    _pingTimer?.cancel();
    _channel?.sink.close();
    _controller.close();
  }
}
