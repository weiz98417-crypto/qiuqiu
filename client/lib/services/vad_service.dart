import 'dart:async';
import 'dart:collection';
import 'dart:math';

import 'package:audio_session/audio_session.dart';
import 'package:flutter/foundation.dart';
import 'package:record/record.dart';

class VADService {
  final _eventController = StreamController<VADEvent>.broadcast();
  final _recorder = AudioRecorder();
  final _audioBuffer = <Uint8List>[];
  final _preRoll = Queue<Uint8List>();

  StreamSubscription<Uint8List>? _recordSubscription;
  Timer? _silenceTimer;
  VADMode _mode = VADMode.pushToTalk;
  bool _sessionActive = false;
  bool _captureActive = false;
  bool _finishingSentence = false;
  bool _disposed = false;
  bool _audioSessionConfigured = false;
  int _speechFrames = 0;

  static const double silenceThreshold = 0.035;
  static const int minSpeechFrames = 4;
  static const int silenceTimeoutMs = 760;
  static const int maxDurationMs = 15000;
  static const int preRollFrames = 4;

  Stream<VADEvent> get events => _eventController.stream;
  bool get isListening => _sessionActive;
  VADMode get mode => _mode;
  String get debugInfo => '';

  Future<bool> hasPermission() => _recorder.hasPermission();

  Future<void> startListening(VADMode mode) async {
    if (_disposed) return;
    if (_sessionActive && _captureActive && _mode == mode) return;
    _mode = mode;
    _sessionActive = true;
    _audioBuffer.clear();
    await _startCapture();
  }

  Future<void> _startCapture() async {
    if (_disposed || !_sessionActive || _captureActive) return;
    if (!_audioSessionConfigured) {
      final audioSession = await AudioSession.instance;
      await audioSession.configure(const AudioSessionConfiguration.speech());
      _audioSessionConfigured = true;
    }
    final permitted = await _recorder.hasPermission();
    if (!permitted) {
      _sessionActive = false;
      _emit(const VADEvent.permissionDenied());
      return;
    }

    _speechFrames = 0;
    _preRoll.clear();
    _silenceTimer?.cancel();
    try {
      final stream = await _recorder.startStream(
        const RecordConfig(
          encoder: AudioEncoder.pcm16bits,
          sampleRate: 16000,
          numChannels: 1,
        ),
      );
      if (!_sessionActive || _disposed) {
        await _recorder.stop();
        return;
      }
      _captureActive = true;
      _emit(const VADEvent.listening());
      _recordSubscription = stream.listen(
        _processAudio,
        onError: (Object error) {
          _emit(VADEvent.failure(error.toString()));
          stopListening();
        },
      );
    } catch (error) {
      _sessionActive = false;
      _captureActive = false;
      _emit(VADEvent.failure(error.toString()));
    }
  }

  void _processAudio(Uint8List pcm) {
    if (!_sessionActive || !_captureActive || pcm.isEmpty) return;
    final rms = _calculateRms(pcm);
    final speechDetected = rms > silenceThreshold;

    if (_mode == VADMode.pushToTalk) {
      _audioBuffer.add(pcm);
    } else if (_speechFrames < minSpeechFrames) {
      _preRoll.addLast(pcm);
      while (_preRoll.length > preRollFrames) {
        _preRoll.removeFirst();
      }
    } else {
      _audioBuffer.add(pcm);
    }

    if (speechDetected) {
      _speechFrames++;
      _silenceTimer?.cancel();
      _silenceTimer = null;
      if (_speechFrames == minSpeechFrames) {
        if (_mode == VADMode.freeTalk) {
          _audioBuffer.addAll(_preRoll);
          _preRoll.clear();
        }
        _emit(const VADEvent.speaking());
      }
      if (_speechFrames >= maxDurationMs ~/ 50) {
        unawaited(_finishSentence());
      }
    } else if (_speechFrames >= minSpeechFrames) {
      _silenceTimer ??= Timer(
        const Duration(milliseconds: silenceTimeoutMs),
        () => unawaited(_finishSentence()),
      );
    }
  }

  double _calculateRms(Uint8List pcm) {
    if (pcm.length < 2) return 0;
    var sum = 0.0;
    final samples = pcm.length ~/ 2;
    for (var offset = 0; offset < pcm.length - 1; offset += 2) {
      final unsigned = (pcm[offset + 1] << 8) | pcm[offset];
      final signed = unsigned > 32767 ? unsigned - 65536 : unsigned;
      final normalized = signed / 32768.0;
      sum += normalized * normalized;
    }
    return sqrt(sum / samples);
  }

  Future<void> _finishSentence() async {
    if (_finishingSentence || !_sessionActive) return;
    _finishingSentence = true;
    _silenceTimer?.cancel();
    _silenceTimer = null;
    await _stopCapture();

    final hasSpeech = _speechFrames >= minSpeechFrames;
    if (hasSpeech || _mode == VADMode.pushToTalk) {
      _emit(const VADEvent.sentenceEnd());
    }

    if (_mode == VADMode.pushToTalk) {
      _sessionActive = false;
    } else if (_sessionActive && !_disposed) {
      await Future<void>.delayed(const Duration(milliseconds: 160));
      await _startCapture();
    }
    _finishingSentence = false;
  }

  void onManualStop() {
    unawaited(_finishSentence());
  }

  void stopListening() {
    if (!_sessionActive && !_captureActive) return;
    _sessionActive = false;
    _silenceTimer?.cancel();
    _silenceTimer = null;
    _speechFrames = 0;
    _preRoll.clear();
    unawaited(_stopCapture());
    _emit(const VADEvent.idle());
  }

  Future<void> _stopCapture() async {
    _captureActive = false;
    try {
      await _recordSubscription?.cancel();
      _recordSubscription = null;
      await _recorder.stop();
    } catch (_) {
      return;
    }
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
    if (_mode == VADMode.freeTalk) unawaited(_finishSentence());
  }

  void onAudioData(Uint8List pcm) => _processAudio(pcm);

  void _emit(VADEvent event) {
    if (!_disposed && !_eventController.isClosed) {
      _eventController.add(event);
    }
  }

  void dispose() {
    if (_disposed) return;
    stopListening();
    _disposed = true;
    unawaited(_eventController.close());
    unawaited(_recorder.dispose());
  }
}

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
