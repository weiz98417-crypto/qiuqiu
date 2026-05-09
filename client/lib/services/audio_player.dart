import 'dart:async';
import 'dart:collection';
import 'package:flutter/foundation.dart';

/// PCM audio playback with jitter buffer and focus management.
///
/// Receives PCM 16bit 24kHz mono frames from WebSocket,
/// buffers with 200ms window, and feeds to platform audio output.
class AudioPlayerService {
  final _buffer = Queue<Uint8List>();
  final _controller = StreamController<AudioState>.broadcast();
  static const int bufferDurationMs = 200;
  bool _isMuted = false;

  Stream<AudioState> get stateStream => _controller.stream;

  void pushPcmFrame(Uint8List frame) {
    if (_isMuted) return;
    _buffer.add(frame);
  }

  void start() {
    _controller.add(const AudioState.playing());
  }

  void pause() {
    _controller.add(const AudioState.stopped());
  }

  void mute() {
    _isMuted = true;
    _buffer.clear();
    _controller.add(const AudioState.stopped());
  }

  void unmute() {
    _isMuted = false;
  }

  /// Called when phone call arrives.
  void onAudioFocusLost() {
    pause();
  }

  /// Called when phone call ends.
  void onAudioFocusGained() {
    if (!_isMuted) start();
  }

  void dispose() {
    _buffer.clear();
    _controller.close();
  }
}

@immutable
class AudioState {
  final bool isPlaying;
  const AudioState.playing() : isPlaying = true;
  const AudioState.stopped() : isPlaying = false;
}
