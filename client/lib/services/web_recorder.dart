import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:web/web.dart' as web;

enum VADMode { pushToTalk, freeTalk }

@immutable
class VADEvent {
  final VADState state;
  final String? message;

  const VADEvent({required this.state, this.message});
  const VADEvent.listening() : this(state: VADState.listening);
  const VADEvent.speaking() : this(state: VADState.speaking);
  const VADEvent.sentenceEnd() : this(state: VADState.sentenceEnd);
  const VADEvent.idle() : this(state: VADState.idle);
  const VADEvent.permissionDenied() : this(state: VADState.permissionDenied);
  const VADEvent.failure(String message)
      : this(state: VADState.failure, message: message);
}

enum VADState {
  listening,
  speaking,
  sentenceEnd,
  idle,
  permissionDenied,
  failure,
}

@immutable
class AudioInputDevice {
  final String id;
  final String label;

  const AudioInputDevice({required this.id, required this.label});
}

void _runJavaScript(String code) {
  final script = web.HTMLScriptElement()..text = code;
  final head = web.document.head;
  if (head == null) return;
  head.appendChild(script);
  script.remove();
}

List<Map<String, dynamic>> _readRecorderEvents() {
  final bridge = web.document.getElementById('__qDiv');
  if (bridge == null) return const [];
  _runJavaScript(
    "document.getElementById('__qDiv').textContent=JSON.stringify(window.__qRecDrain?window.__qRecDrain():[])",
  );
  final raw = bridge.textContent;
  if (raw == null || raw.isEmpty || raw == 'null') return const [];
  final decoded = jsonDecode(raw);
  if (decoded is! List) return const [];
  return decoded
      .whereType<Map>()
      .map((event) => Map<String, dynamic>.from(event))
      .toList(growable: false);
}

class VADService {
  final _eventController = StreamController<VADEvent>.broadcast(sync: true);
  final _audioChunkController =
      StreamController<Uint8List>.broadcast(sync: true);
  final _inputDeviceController =
      StreamController<List<AudioInputDevice>>.broadcast(sync: true);
  final _audioBuffer = <Uint8List>[];
  Timer? _eventPoll;
  VADMode _mode = VADMode.pushToTalk;
  bool _sessionActive = false;
  bool _captureActive = false;
  bool _disposed = false;
  String _selectedInputDeviceId = '';
  String _activeInputDeviceId = '';
  List<AudioInputDevice> _inputDeviceList = const [
    AudioInputDevice(id: '', label: '系统默认麦克风'),
  ];
  Completer<List<AudioInputDevice>>? _deviceRefreshCompleter;

  Stream<VADEvent> get events => _eventController.stream;
  Stream<Uint8List> get audioChunks => _audioChunkController.stream;
  Stream<List<AudioInputDevice>> get inputDevices =>
      _inputDeviceController.stream;
  bool get isListening => _sessionActive;
  VADMode get mode => _mode;
  String get debugInfo => '';
  String get selectedInputDeviceId => _selectedInputDeviceId;
  String get activeInputDeviceId => _activeInputDeviceId;

  Future<bool> hasPermission() async => true;

  Future<List<AudioInputDevice>> refreshInputDevices() {
    if (_disposed) return Future.value(_inputDeviceList);
    final existing = _deviceRefreshCompleter;
    if (existing != null && !existing.isCompleted) return existing.future;
    final completer = Completer<List<AudioInputDevice>>();
    _deviceRefreshCompleter = completer;
    _ensureEventPoll();
    _runJavaScript(
      'if(window.__qRecRequestDevices)window.__qRecRequestDevices(true)',
    );
    return completer.future.timeout(
      const Duration(seconds: 4),
      onTimeout: () {
        if (identical(_deviceRefreshCompleter, completer)) {
          _deviceRefreshCompleter = null;
        }
        return _inputDeviceList;
      },
    );
  }

  Future<void> selectInputDevice(String deviceId) async {
    if (_disposed) return;
    _selectedInputDeviceId = deviceId;
    final restartCapture = _sessionActive;
    if (_captureActive) {
      _captureActive = false;
      _runJavaScript('if(window.__qRec)window.__qRec(false)');
    }
    _runJavaScript(
      'if(window.__qRecSelectDevice)window.__qRecSelectDevice(${jsonEncode(deviceId)})',
    );
    if (restartCapture) _startBrowserCapture();
  }

  Future<void> startListening(VADMode mode) async {
    if (_disposed || (_sessionActive && _captureActive && _mode == mode)) {
      return;
    }
    _mode = mode;
    _sessionActive = true;
    _audioBuffer.clear();
    _startBrowserCapture();
  }

  void _startBrowserCapture() {
    if (_disposed || !_sessionActive || _captureActive) return;
    _captureActive = true;
    _ensureEventPoll();
    _runJavaScript(
      "if(window.__qRecStart){window.__qRecStart()}else if(window.__qRecEmit){window.__qRecEmit({e:'error',message:'recorder unavailable'})}",
    );
  }

  void _ensureEventPoll() {
    _eventPoll ??= Timer.periodic(
      const Duration(milliseconds: 80),
      (_) => _checkRecorderEvent(),
    );
  }

  void _checkRecorderEvent() {
    try {
      for (final data in _readRecorderEvents()) {
        _handleRecorderEvent(data);
      }
    } catch (error) {
      _emit(VADEvent.failure(error.toString()));
    }
  }

  void _handleRecorderEvent(Map<String, dynamic> data) {
    final event = data['e'] as String?;
    final audio = data['a'] as String?;
    if (event == 'devices') {
      final devices = (data['devices'] as List?)
              ?.whereType<Map>()
              .map(
                (device) => AudioInputDevice(
                  id: device['id']?.toString() ?? '',
                  label: device['label']?.toString() ?? '麦克风',
                ),
              )
              .toList(growable: false) ??
          const <AudioInputDevice>[];
      if (devices.isNotEmpty) _inputDeviceList = devices;
      _selectedInputDeviceId = data['selectedId']?.toString() ?? '';
      final activeId = data['activeId'];
      if (activeId != null) _activeInputDeviceId = activeId.toString();
      _inputDeviceController.add(_inputDeviceList);
      final completer = _deviceRefreshCompleter;
      _deviceRefreshCompleter = null;
      if (completer != null && !completer.isCompleted) {
        completer.complete(_inputDeviceList);
      }
    } else if (event == 'deviceError') {
      final completer = _deviceRefreshCompleter;
      _deviceRefreshCompleter = null;
      if (completer != null && !completer.isCompleted) {
        completer.completeError(
          StateError(data['message']?.toString() ?? '无法读取麦克风列表'),
        );
      }
    } else if (event == 'listening') {
      _selectedInputDeviceId =
          data['selectedId']?.toString() ?? _selectedInputDeviceId;
      _activeInputDeviceId = data['deviceId']?.toString() ?? '';
      if (_sessionActive) _emit(const VADEvent.listening());
    } else if (event == 'speaking') {
      _emit(const VADEvent.speaking());
    } else if (event == 'audioChunk') {
      if (audio != null && audio.isNotEmpty) {
        _emitAudioChunk(base64Decode(audio));
      }
    } else if (event == 'sentenceEnd') {
      _captureActive = false;
      if (audio != null && audio.isNotEmpty) {
        _audioBuffer.add(base64Decode(audio));
      }
      _emit(const VADEvent.sentenceEnd());
      if (_mode == VADMode.pushToTalk) {
        _sessionActive = false;
      } else if (_sessionActive) {
        Timer(const Duration(milliseconds: 160), _startBrowserCapture);
      }
    } else if (event == 'permissionDenied') {
      _captureActive = false;
      _sessionActive = false;
      _emit(const VADEvent.permissionDenied());
    } else if (event == 'error') {
      _captureActive = false;
      _sessionActive = false;
      _emit(
        VADEvent.failure(data['message']?.toString() ?? 'recording failed'),
      );
    }
  }

  void stopListening() {
    if (!_sessionActive && !_captureActive) return;
    _sessionActive = false;
    _captureActive = false;
    _runJavaScript('if(window.__qRec)window.__qRec(false)');
    _emit(const VADEvent.idle());
  }

  void onManualStop() {
    if (!_sessionActive) return;
    _captureActive = false;
    _runJavaScript('if(window.__qRec)window.__qRec(true)');
  }

  Uint8List? drainAudio() {
    if (_audioBuffer.isEmpty) return null;
    final totalLength = _audioBuffer.fold<int>(
      0,
      (length, chunk) => length + chunk.length,
    );
    final result = Uint8List(totalLength);
    var offset = 0;
    for (final chunk in _audioBuffer) {
      result.setRange(offset, offset + chunk.length, chunk);
      offset += chunk.length;
    }
    _audioBuffer.clear();
    return result;
  }

  void onSpeechDetected() {
    if (_sessionActive) _emit(const VADEvent.speaking());
  }

  void onSilenceTimeout() {
    if (_mode == VADMode.freeTalk) {
      _runJavaScript('if(window.__qRec)window.__qRec(true)');
    }
  }

  void onAudioData(Uint8List pcm) {}

  void _emit(VADEvent event) {
    if (!_disposed && !_eventController.isClosed) {
      _eventController.add(event);
    }
  }

  void _emitAudioChunk(Uint8List audio) {
    if (!_disposed && !_audioChunkController.isClosed && audio.isNotEmpty) {
      _audioChunkController.add(audio);
    }
  }

  void dispose() {
    if (_disposed) return;
    stopListening();
    _disposed = true;
    _eventPoll?.cancel();
    final completer = _deviceRefreshCompleter;
    if (completer != null && !completer.isCompleted) {
      completer.complete(_inputDeviceList);
    }
    unawaited(_eventController.close());
    unawaited(_audioChunkController.close());
    unawaited(_inputDeviceController.close());
  }
}
