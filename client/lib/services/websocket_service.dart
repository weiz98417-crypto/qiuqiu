import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:web_socket_channel/web_socket_channel.dart';

enum SocketStatus { connecting, connected, reconnecting, disconnected, failed }

class WebSocketService {
  final _messageController = StreamController<Map<String, dynamic>>.broadcast();
  final _binaryController = StreamController<Uint8List>.broadcast();
  final _statusController = StreamController<SocketStatus>.broadcast();

  WebSocketChannel? _channel;
  StreamSubscription<dynamic>? _channelSubscription;
  Timer? _pingTimer;
  Timer? _reconnectTimer;
  String? _url;
  String _token = '';
  int _reconnectAttempts = 0;
  int _connectionGeneration = 0;
  bool _connected = false;
  bool _disposed = false;

  static const int _maxReconnectAttempts = 5;

  Stream<Map<String, dynamic>> get onMessage => _messageController.stream;
  Stream<Uint8List> get onBinary => _binaryController.stream;
  Stream<SocketStatus> get statusStream => _statusController.stream;

  void connect(String url, {String token = ''}) {
    if (_disposed) return;
    _url = url;
    _token = token;
    _reconnectAttempts = 0;
    _reconnectTimer?.cancel();
    _open(url, reconnecting: false);
  }

  Future<void> _open(String url, {required bool reconnecting}) async {
    if (_disposed) return;
    final generation = ++_connectionGeneration;
    _connected = false;
    _emitStatus(
      reconnecting ? SocketStatus.reconnecting : SocketStatus.connecting,
    );

    await _channelSubscription?.cancel();
    await _channel?.sink.close();

    try {
      final protocols = _token.isEmpty
          ? null
          : <String>['qiuqiu-auth.${_encodeToken(_token)}'];
      final channel = WebSocketChannel.connect(
        Uri.parse(url),
        protocols: protocols,
      );
      _channel = channel;
      await channel.ready;
      if (_disposed || generation != _connectionGeneration) {
        await channel.sink.close();
        return;
      }

      _reconnectAttempts = 0;
      _connected = true;
      _emitStatus(SocketStatus.connected);
      _startHeartbeat();
      _channelSubscription = channel.stream.listen(
        (data) => _handleData(data, generation),
        onError: (_) => _scheduleReconnect(generation),
        onDone: () => _scheduleReconnect(generation),
        cancelOnError: true,
      );
    } catch (_) {
      _scheduleReconnect(generation);
    }
  }

  String _encodeToken(String token) {
    return base64Url.encode(utf8.encode(token)).replaceAll('=', '');
  }

  void _handleData(dynamic data, int generation) {
    if (_disposed || generation != _connectionGeneration) return;
    if (data is String) {
      try {
        final decoded = jsonDecode(data);
        if (decoded is Map<String, dynamic>) {
          _messageController.add(decoded);
        }
      } catch (_) {
        return;
      }
    } else if (data is List<int>) {
      _binaryController.add(Uint8List.fromList(data));
    }
  }

  void _startHeartbeat() {
    _pingTimer?.cancel();
    _pingTimer = Timer.periodic(const Duration(seconds: 25), (_) {
      send({'type': 'ping'});
    });
  }

  void _scheduleReconnect(int generation) {
    if (_disposed || generation != _connectionGeneration) return;
    _connected = false;
    _pingTimer?.cancel();
    if (_reconnectTimer?.isActive ?? false) return;
    final url = _url;
    if (url == null || _reconnectAttempts >= _maxReconnectAttempts) {
      _emitStatus(SocketStatus.failed);
      return;
    }

    _reconnectAttempts++;
    final seconds = (1 << (_reconnectAttempts - 1)).clamp(1, 8);
    _emitStatus(SocketStatus.reconnecting);
    _messageController.add({
      'type': 'reconnecting',
      'attempt': _reconnectAttempts,
    });
    _reconnectTimer = Timer(Duration(seconds: seconds), () {
      if (_disposed || generation != _connectionGeneration) return;
      _open(url, reconnecting: true);
    });
  }

  bool send(Map<String, dynamic> message) {
    if (_disposed || !_connected || _channel == null) return false;
    try {
      _channel!.sink.add(jsonEncode(message));
      return true;
    } catch (_) {
      return false;
    }
  }

  void _emitStatus(SocketStatus status) {
    if (!_disposed && !_statusController.isClosed) {
      _statusController.add(status);
    }
  }

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    _connected = false;
    _connectionGeneration++;
    _reconnectTimer?.cancel();
    _pingTimer?.cancel();
    await _channelSubscription?.cancel();
    await _channel?.sink.close();
    await _messageController.close();
    await _binaryController.close();
    await _statusController.close();
  }
}
