import 'dart:async';
import 'dart:collection';
import 'dart:typed_data';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter_soloud/flutter_soloud.dart';

/// PCM audio playback with jitter buffer and focus management.
///
/// Receives PCM 16bit 24kHz mono frames, buffers with 200ms window,
/// and plays through flutter_soloud.
class AudioPlayerService {
  final _buffer = Queue<Uint8List>();
  final _controller = StreamController<AudioState>.broadcast();
  static const int bufferDurationMs = 200;
  static const int sampleRate = 24000;
  bool _isMuted = false;
  bool _isPlaying = false;
  bool _initialized = false;
  Timer? _drainTimer;

  SoLoud? _soloud;
  final List<SoundHandle> _activeSounds = [];

  Stream<AudioState> get stateStream => _controller.stream;

  Future<void> _init() async {
    if (_initialized) return;
    _soloud = SoLoud.instance;
    await _soloud!.init();
    _initialized = true;
  }

  /// Build WAV bytes from raw PCM 16bit mono data.
  Uint8List _pcmToWav(Uint8List pcm) {
    final dataSize = pcm.length;
    final wav = BytesBuilder(copy: false);
    // RIFF header
    wav.add(Uint8List.fromList('RIFF'.codeUnits));
    wav.add(_u32le(dataSize + 36));
    wav.add(Uint8List.fromList('WAVE'.codeUnits));
    // fmt chunk
    wav.add(Uint8List.fromList('fmt '.codeUnits));
    wav.add(_u32le(16));
    wav.add(_u16le(1)); // PCM
    wav.add(_u16le(1)); // mono
    wav.add(_u32le(sampleRate));
    wav.add(_u32le(sampleRate * 2));
    wav.add(_u16le(2));
    wav.add(_u16le(16));
    // data chunk
    wav.add(Uint8List.fromList('data'.codeUnits));
    wav.add(_u32le(dataSize));
    wav.add(pcm);
    return wav.takeBytes();
  }

  Uint8List _u32le(int v) {
    final b = Uint8List(4);
    b[0] = v & 0xff; b[1] = (v >> 8) & 0xff;
    b[2] = (v >> 16) & 0xff; b[3] = (v >> 24) & 0xff;
    return b;
  }

  Uint8List _u16le(int v) {
    final b = Uint8List(2);
    b[0] = v & 0xff; b[1] = (v >> 8) & 0xff;
    return b;
  }

  void pushPcmFrame(Uint8List frame) {
    if (_isMuted) return;
    _buffer.add(frame);
    if (_buffer.length >= 2 && (_drainTimer == null || !_drainTimer!.isActive)) {
      _startDrain();
    }
  }

  void _startDrain() {
    _drainTimer?.cancel();
    _drainTimer = Timer.periodic(const Duration(milliseconds: 200), (_) => _drain());
  }

  Future<void> _drain() async {
    if (_buffer.isEmpty) {
      _drainTimer?.cancel();
      if (_isPlaying && _activeSounds.isEmpty) {
        _isPlaying = false;
        _controller.add(const AudioState.stopped());
      }
      return;
    }
    await _init();
    // Accumulate all available chunks
    final totalLen = _buffer.fold<int>(0, (s, b) => s + b.length);
    final combined = Uint8List(totalLen);
    var off = 0;
    while (_buffer.isNotEmpty) {
      final c = _buffer.removeFirst();
      combined.setRange(off, off + c.length, c);
      off += c.length;
    }
    final wav = _pcmToWav(combined);
    try {
      final source = await _soloud!.loadMem("chunk", wav, mode: LoadMode.memory);
      final handle = await _soloud!.play(source);
      _activeSounds.add(handle);
      // Clean finished sounds
      _activeSounds.removeWhere((h) => !_soloud!.getIsValidVoiceHandle(h));
    } on PlatformException {
      // Audio device not ready, retry next cycle
    }
  }

  void start() {
    _isPlaying = true;
    _controller.add(const AudioState.playing());
    if (_buffer.isNotEmpty) _startDrain();
  }

  void pause() {
    _isPlaying = false;
    _drainTimer?.cancel();
    _stopAllSounds();
    _controller.add(const AudioState.stopped());
  }

  void mute() {
    _isMuted = true;
    _buffer.clear();
    _drainTimer?.cancel();
    _stopAllSounds();
    _controller.add(const AudioState.stopped());
  }

  void unmute() {
    _isMuted = false;
  }

  void _stopAllSounds() {
    for (final h in _activeSounds) {
      try { _soloud?.stop(h); } catch (_) {}
    }
    _activeSounds.clear();
  }

  void onAudioFocusLost() => pause();
  void onAudioFocusGained() { if (!_isMuted) start(); }

  void dispose() {
    _drainTimer?.cancel();
    _stopAllSounds();
    _buffer.clear();
    _controller.close();
    _soloud?.deinit();
    _initialized = false;
  }
}

@immutable
class AudioState {
  final bool isPlaying;
  const AudioState.playing() : isPlaying = true;
  const AudioState.stopped() : isPlaying = false;
}
