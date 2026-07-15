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

void _runJavaScript(String code) {
  final script = web.HTMLScriptElement()..text = code;
  final head = web.document.head;
  if (head == null) return;
  head.appendChild(script);
  script.remove();
}

String? _readRecorderEvent() {
  final bridge = web.document.getElementById('__qDiv');
  if (bridge == null) return null;
  final raw = bridge.textContent;
  if (raw == null || raw.isEmpty || raw == 'null') return null;
  _runJavaScript("document.getElementById('__qDiv').textContent='null'");
  return raw;
}

class VADService {
  final _eventController = StreamController<VADEvent>.broadcast();
  final _audioBuffer = <Uint8List>[];
  Timer? _eventPoll;
  VADMode _mode = VADMode.pushToTalk;
  bool _sessionActive = false;
  bool _captureActive = false;
  bool _disposed = false;

  Stream<VADEvent> get events => _eventController.stream;
  bool get isListening => _sessionActive;
  VADMode get mode => _mode;
  String get debugInfo => '';

  Future<bool> hasPermission() async => true;

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
    _eventPoll ??= Timer.periodic(
      const Duration(milliseconds: 80),
      (_) => _checkRecorderEvent(),
    );
    _runJavaScript('window.__qRecStart()');
    _emit(const VADEvent.listening());
  }

  void _checkRecorderEvent() {
    final raw = _readRecorderEvent();
    if (raw == null) return;
    try {
      final data = jsonDecode(raw) as Map<String, dynamic>;
      final event = data['e'] as String?;
      final audio = data['a'] as String?;
      if (event == 'speaking') {
        _emit(const VADEvent.speaking());
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
    } catch (error) {
      _emit(VADEvent.failure(error.toString()));
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

  void dispose() {
    if (_disposed) return;
    stopListening();
    _disposed = true;
    _eventPoll?.cancel();
    unawaited(_eventController.close());
  }
}
