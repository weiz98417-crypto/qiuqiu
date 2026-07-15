import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_soloud/flutter_soloud.dart';

enum AudioPlaybackStatus { started, ended, interrupted, blocked, failed }

@immutable
class AudioState {
  final AudioPlaybackStatus status;
  final String? traceId;
  final String? error;

  const AudioState({required this.status, this.traceId, this.error});
}

class AudioPlayerService {
  final _stateController = StreamController<AudioState>.broadcast();
  final _activeHandles = <SoundHandle>[];
  SoLoud? _soloud;
  Timer? _completionTimer;
  bool _initialized = false;
  bool _muted = false;
  bool _disposed = false;
  String? _currentTraceId;

  Stream<AudioState> get stateStream => _stateController.stream;
  bool get isMuted => _muted;

  Future<void> _initialize() async {
    if (_initialized) return;
    _soloud = SoLoud.instance;
    await _soloud!.init();
    _initialized = true;
  }

  Future<void> playEncoded(
    Uint8List audio, {
    required String mime,
    String? traceId,
    double speed = 1,
  }) async {
    if (_muted || _disposed || audio.isEmpty) return;
    await pause(notify: false);
    try {
      await _initialize();
      final extension = _extensionForMime(mime);
      final source = await _soloud!.loadMem(
        'qiuqiu-reply.$extension',
        audio,
        mode: LoadMode.memory,
      );
      final handle = _soloud!.play(source);
      _soloud!.setRelativePlaySpeed(handle, speed.clamp(0.8, 1.2).toDouble());
      _activeHandles.add(handle);
      _currentTraceId = traceId;
      _emit(AudioPlaybackStatus.started, traceId: traceId);
      _watchCompletion();
    } catch (error) {
      _emit(
        AudioPlaybackStatus.failed,
        traceId: traceId,
        error: error.toString(),
      );
    }
  }

  String _extensionForMime(String mime) {
    final normalized = mime.toLowerCase();
    if (normalized.contains('mpeg') || normalized.contains('mp3')) return 'mp3';
    if (normalized.contains('ogg')) return 'ogg';
    return 'wav';
  }

  void _watchCompletion() {
    _completionTimer?.cancel();
    _completionTimer = Timer.periodic(const Duration(milliseconds: 100), (_) {
      if (_disposed || !_initialized) return;
      _activeHandles.removeWhere(
        (handle) => !_soloud!.getIsValidVoiceHandle(handle),
      );
      if (_activeHandles.isEmpty) {
        _completionTimer?.cancel();
        final traceId = _currentTraceId;
        _currentTraceId = null;
        _emit(AudioPlaybackStatus.ended, traceId: traceId);
      }
    });
  }

  Future<void> pause({bool notify = true}) async {
    _completionTimer?.cancel();
    for (final handle in _activeHandles) {
      try {
        _soloud?.stop(handle);
      } catch (_) {
        continue;
      }
    }
    _activeHandles.clear();
    final traceId = _currentTraceId;
    _currentTraceId = null;
    if (notify && traceId != null) {
      _emit(AudioPlaybackStatus.interrupted, traceId: traceId);
    }
  }

  Future<void> setMuted(bool muted) async {
    _muted = muted;
    if (muted) await pause();
  }

  void _emit(AudioPlaybackStatus status, {String? traceId, String? error}) {
    if (!_disposed && !_stateController.isClosed) {
      _stateController.add(
        AudioState(status: status, traceId: traceId, error: error),
      );
    }
  }

  Future<void> dispose() async {
    if (_disposed) return;
    await pause(notify: false);
    _disposed = true;
    if (_initialized) {
      _soloud?.deinit();
      _initialized = false;
    }
    await _stateController.close();
  }
}
