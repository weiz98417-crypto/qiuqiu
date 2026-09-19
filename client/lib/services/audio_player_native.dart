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
  final _sources = AudioSourceLifecycle<AudioSource>(
    release: _releaseSource,
  );
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

  /// SoLoud 来源释放：disposeSource 是异步调用，失败不能拖垮播放路径。
  static void _releaseSource(AudioSource source) {
    unawaited(_disposeQuietly(source));
  }

  static Future<void> _disposeQuietly(AudioSource source) async {
    try {
      await SoLoud.instance.disposeSource(source);
    } catch (_) {
      // 单个来源释放失败不拖垮播放路径。
    }
  }

  Future<void> playEncoded(
    Uint8List audio, {
    required String mime,
    String? traceId,
  }) async {
    if (_muted || _disposed || audio.isEmpty) return;
    // 新播放前先停声并释放上一来源，长会话不再累积 AudioSource。
    await pause(notify: false);
    try {
      await _initialize();
      final extension = _extensionForMime(mime);
      final source = await _soloud!.loadMem(
        'qiuqiu-reply.$extension',
        audio,
        mode: LoadMode.memory,
      );
      _sources.attach(source);
      final handle = _soloud!.play(source);
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
    _sources.dispose();
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

/// 音频来源生命周期：单字段跟踪「当前来源」，换源时释放旧来源，dispose
/// 释放当前来源且幂等。释放回调注入——SoLoud 本体不可注入测试，
/// 生命周期类可测：泛型来源 + 注入释放让单测不依赖任何原生音频栈。
class AudioSourceLifecycle<TSource> {
  final void Function(TSource source) _release;

  TSource? _current;

  AudioSourceLifecycle({required void Function(TSource source) release})
      : _release = release;

  TSource? get current => _current;

  /// 记录新的当前来源；若已有不同来源则先释放旧来源。
  void attach(TSource source) {
    final previous = _current;
    _current = source;
    if (previous != null && !identical(previous, source)) {
      _releaseQuietly(previous);
    }
  }

  /// 释放当前来源并停止跟踪；重复调用为空操作（幂等）。
  void dispose() {
    final source = _current;
    _current = null;
    if (source != null) _releaseQuietly(source);
  }

  void _releaseQuietly(TSource source) {
    try {
      _release(source);
    } catch (_) {
      // 释放失败不阻塞播放状态机。
    }
  }
}
