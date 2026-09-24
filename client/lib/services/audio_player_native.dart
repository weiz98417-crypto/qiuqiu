import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_soloud/flutter_soloud.dart';

enum AudioPlaybackStatus { started, ended, interrupted, blocked, failed }

@immutable
class AudioState {
  final AudioPlaybackStatus status;
  final String? traceId;

  /// 下发音频携带的投递键：随播放终态原样回传，供 playback_result 实报
  /// 定位投递记录；旧下发路径缺此键时不实报。
  final String? deliveryKey;
  final String? error;

  const AudioState({
    required this.status,
    this.traceId,
    this.deliveryKey,
    this.error,
  });
}

class AudioPlayerService {
  final _stateController = StreamController<AudioState>.broadcast();
  final _activeHandles = <SoundHandle>[];
  late final _sources = AudioSourceLifecycle<AudioSource>(
    release: _releaseSource,
  );
  /// 在途释放 futures：deinit 前逐一等待，避免对已反初始化引擎释放。
  final _pendingReleases = <Future<void>>[];
  SoLoud? _soloud;
  Timer? _completionTimer;
  bool _initialized = false;
  bool _muted = false;
  bool _disposed = false;
  String? _currentTraceId;
  String? _currentDeliveryKey;

  Stream<AudioState> get stateStream => _stateController.stream;
  bool get isMuted => _muted;

  Future<void> _initialize() async {
    if (_initialized) return;
    _soloud = SoLoud.instance;
    await _soloud!.init();
    _initialized = true;
  }

  /// SoLoud 来源释放：disposeSource 是异步调用，失败不能拖垮播放路径；
  /// future 挂到 _pendingReleases，dispose 前统一等待。
  void _releaseSource(AudioSource source) {
    _pendingReleases.add(_disposeQuietly(source));
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
    String? deliveryKey,
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
      _currentDeliveryKey = deliveryKey;
      _emit(
        AudioPlaybackStatus.started,
        traceId: traceId,
        deliveryKey: deliveryKey,
      );
      _watchCompletion();
    } catch (error) {
      _emit(
        AudioPlaybackStatus.failed,
        traceId: traceId,
        deliveryKey: deliveryKey,
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
        final deliveryKey = _currentDeliveryKey;
        _currentTraceId = null;
        _currentDeliveryKey = null;
        _emit(
          AudioPlaybackStatus.ended,
          traceId: traceId,
          deliveryKey: deliveryKey,
        );
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
    final deliveryKey = _currentDeliveryKey;
    _currentTraceId = null;
    _currentDeliveryKey = null;
    if (notify && (traceId != null || deliveryKey != null)) {
      _emit(
        AudioPlaybackStatus.interrupted,
        traceId: traceId,
        deliveryKey: deliveryKey,
      );
    }
  }

  Future<void> setMuted(bool muted) async {
    _muted = muted;
    if (muted) await pause();
  }

  void _emit(
    AudioPlaybackStatus status, {
    String? traceId,
    String? deliveryKey,
    String? error,
  }) {
    if (!_disposed && !_stateController.isClosed) {
      _stateController.add(
        AudioState(
          status: status,
          traceId: traceId,
          deliveryKey: deliveryKey,
          error: error,
        ),
      );
    }
  }

  Future<void> dispose() async {
    if (_disposed) return;
    await pause(notify: false);
    _disposed = true;
    await Future.wait(_pendingReleases);
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
