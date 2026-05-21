import 'dart:async';
import 'dart:convert';
import 'dart:html' as html;
import 'dart:typed_data';
import 'package:flutter/foundation.dart';

enum VADMode { pushToTalk, freeTalk }

@immutable
class VADEvent {
  final VADState state;
  const VADEvent({required this.state});
  const VADEvent.listening() : state = VADState.listening;
  const VADEvent.speaking() : state = VADState.speaking;
  const VADEvent.sentenceEnd() : state = VADState.sentenceEnd;
  const VADEvent.idle() : state = VADState.idle;
}

enum VADState { listening, speaking, sentenceEnd, idle }

void _runJS(String code) {
  final s = html.ScriptElement()..text = code;
  final head = html.document.querySelector('head');
  if (head != null) {
    head.append(s);
    s.remove();
  }
}

String? _readVAD() {
  final div = html.document.getElementById('__qDiv');
  if (div == null) return null;
  final raw = div.text;
  if (raw == null || raw.isEmpty || raw == 'null') return null;
  _runJS("document.getElementById('__qDiv').textContent='null'");
  return raw;
}

class VADService {
  final _controller = StreamController<VADEvent>.broadcast();
  final _audioBuffer = <Uint8List>[];
  bool _isListening = false;
  VADMode _mode = VADMode.pushToTalk;
  Timer? _vadPoll;

  Stream<VADEvent> get events => _controller.stream;
  bool get isListening => _isListening;
  VADMode get mode => _mode;

  Future<bool> hasPermission() async => true;

  Future<void> startListening(VADMode mode) async {
    if (_isListening) return;
    _mode = mode;
    _isListening = true;
    _audioBuffer.clear();
    _controller.add(const VADEvent.listening());
    _vadPoll?.cancel();
    _vadPoll = Timer.periodic(const Duration(milliseconds: 100), (_) => _checkVAD());
    _runJS('window.__qRecStart()');
  }

  int _pollCount = 0;
  String get debugInfo => 'poll:$_pollCount rec:$_isListening buf:${_audioBuffer.length}';

  void _checkVAD() {
    _pollCount++;
    final raw = _readVAD();
    if (raw == null) return;
    try {
      final data = jsonDecode(raw) as Map<String, dynamic>;
      final event = data['e'] as String?;
      final audio = data['a'] as String?;
      if (event == 'speaking') {
        _controller.add(const VADEvent.speaking());
      } else if (event == 'sentenceEnd') {
        if (audio != null) {
          _audioBuffer.add(base64Decode(audio));
        }
        _controller.add(const VADEvent.sentenceEnd());
        if (_mode == VADMode.pushToTalk) _isListening = false;
        if (_mode == VADMode.freeTalk && _isListening) {
          Future.delayed(const Duration(milliseconds: 200), () {
            if (_isListening) startListening(VADMode.freeTalk);
          });
        }
      }
    } catch (_) {}
  }

  void stopListening() {
    _isListening = false;
    _vadPoll?.cancel();
    _runJS('if(window.__qRec)window.__qRec()');
    _controller.add(const VADEvent.idle());
  }

  void onManualStop() {
    _isListening = false;
    _vadPoll?.cancel();
    _runJS('if(window.__qRec)window.__qRec()');
    _controller.add(const VADEvent.sentenceEnd());
  }

  Uint8List? drainAudio() {
    if (_audioBuffer.isEmpty) return null;
    final totalLen = _audioBuffer.fold<int>(0, (s, b) => s + b.length);
    final result = Uint8List(totalLen);
    int offset = 0;
    for (final chunk in _audioBuffer) {
      result.setRange(offset, offset + chunk.length, chunk);
      offset += chunk.length;
    }
    _audioBuffer.clear();
    return result;
  }

  void onSpeechDetected() {
    if (!_isListening) return;
    _controller.add(const VADEvent.speaking());
  }

  void onSilenceTimeout() {
    if (_mode == VADMode.freeTalk) {
      _controller.add(const VADEvent.sentenceEnd());
    }
  }

  void onAudioData(Uint8List pcm) {}

  void dispose() {
    _vadPoll?.cancel();
    stopListening();
    _controller.close();
  }
}
