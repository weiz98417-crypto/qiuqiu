import 'dart:async';
import 'dart:math';
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:record/record.dart';

/// Voice Activity Detection + audio recording state machine.
class VADService {
  final _controller = StreamController<VADEvent>.broadcast();
  final _audioController = StreamController<Uint8List>.broadcast();
  final _recorder = AudioRecorder();
  bool _isListening = false;
  VADMode _mode = VADMode.pushToTalk;
  Timer? _silenceTimer;
  int _speechFrames = 0;
  final _audioBuffer = <Uint8List>[];
  StreamSubscription<Uint8List>? _recordSub;

  static const double silenceThreshold = 0.05;
  static const int minSpeechFrames = 5;
  static const int silenceTimeoutMs = 800;
  static const int maxDurationMs = 15000;

  Stream<VADEvent> get events => _controller.stream;
  bool get isListening => _isListening;
  VADMode get mode => _mode;
  String get debugInfo => '';

  Future<bool> hasPermission() => _recorder.hasPermission();

  Future<void> startListening(VADMode mode) async {
    if (_isListening) return;
    _mode = mode;
    _isListening = true;
    _speechFrames = 0;
    _audioBuffer.clear();
    _controller.add(const VADEvent.listening());

    await _recorder.hasPermission();

    final stream = await _recorder.startStream(
      const RecordConfig(
        encoder: AudioEncoder.pcm16bits,
        sampleRate: 16000,
        numChannels: 1,
      ),
    );

    _recordSub = stream.listen((data) {
      if (!_isListening) return;
      _processAudio(data);
    });
  }

  void _processAudio(Uint8List pcm) {
    final rms = _calcRms(pcm);
    final isSpeech = rms > silenceThreshold;

    if (isSpeech) {
      _speechFrames++;
      _audioBuffer.add(pcm);
      _silenceTimer?.cancel();
      if (_speechFrames == minSpeechFrames) {
        _controller.add(const VADEvent.speaking());
      }
      if (_speechFrames > maxDurationMs ~/ 50) {
        _finishSentence();
      }
    } else if (_speechFrames >= minSpeechFrames) {
      _audioBuffer.add(pcm);
      _silenceTimer ??= Timer(const Duration(milliseconds: silenceTimeoutMs), () {
        _finishSentence();
      });
    }
  }

  double _calcRms(Uint8List pcm) {
    if (pcm.length < 2) return 0;
    var sum = 0.0;
    final samples = pcm.length ~/ 2;
    for (var i = 0; i < pcm.length - 1; i += 2) {
      final s = (pcm[i + 1] << 8) | pcm[i];
      final v = (s > 32767) ? s - 65536 : s;
      sum += (v / 32768.0) * (v / 32768.0);
    }
    return sqrt(sum / samples);
  }

  void _finishSentence() {
    _silenceTimer?.cancel();
    _silenceTimer = null;
    _stopRecording();
    if (_mode == VADMode.freeTalk || _mode == VADMode.pushToTalk) {
      _controller.add(const VADEvent.sentenceEnd());
    }
    if (_mode == VADMode.pushToTalk) {
      _isListening = false;
    }
    if (_mode == VADMode.freeTalk && _isListening) {
      Future.delayed(const Duration(milliseconds: 200), () {
        if (_isListening) startListening(VADMode.freeTalk);
      });
    }
  }

  void onManualStop() => _finishSentence();

  void stopListening() {
    _isListening = false;
    _silenceTimer?.cancel();
    _silenceTimer = null;
    _speechFrames = 0;
    _stopRecording();
    _controller.add(const VADEvent.idle());
  }

  Future<void> _stopRecording() async {
    try {
      await _recordSub?.cancel();
      await _recorder.stop();
    } catch (_) {}
  }

  Uint8List? drainAudio() {
    if (_audioBuffer.isEmpty) return null;
    final totalLen = _audioBuffer.fold<int>(0, (sum, b) => sum + b.length);
    final result = Uint8List(totalLen);
    var offset = 0;
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

  void onAudioData(Uint8List pcm) => _processAudio(pcm);

  void dispose() {
    stopListening();
    _controller.close();
    _audioController.close();
    _recorder.dispose();
  }
}

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
