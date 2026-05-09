import 'dart:async';
import 'package:flutter/foundation.dart';

/// Voice Activity Detection + audio recording state machine.
///
/// Modes:
/// - pushToTalk: user holds mic button, releases to send
/// - freeTalk: tap to enter continuous listening, VAD auto-splits sentences
class VADService {
  final _controller = StreamController<VADEvent>.broadcast();
  bool _isListening = false;
  VADMode _mode = VADMode.pushToTalk;

  Stream<VADEvent> get events => _controller.stream;
  bool get isListening => _isListening;
  VADMode get mode => _mode;

  void startListening(VADMode mode) {
    _mode = mode;
    _isListening = true;
    _controller.add(const VADEvent.listening());
  }

  void stopListening() {
    _isListening = false;
    _controller.add(const VADEvent.idle());
  }

  /// Called when audio level crosses above silence threshold.
  void onSpeechDetected() {
    if (!_isListening) return;
    _controller.add(const VADEvent.speaking());
  }

  /// Called when silence exceeds timeout (800ms) → sentence complete.
  void onSilenceTimeout() {
    if (_mode == VADMode.freeTalk) {
      _controller.add(const VADEvent.sentenceEnd());
    }
  }

  /// Called when user releases mic button in pushToTalk mode.
  void onManualStop() {
    _controller.add(const VADEvent.sentenceEnd());
  }

  void dispose() {
    _controller.close();
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
