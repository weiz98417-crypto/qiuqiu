import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:web/web.dart' as web;

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
  Timer? _eventPoll;
  bool _muted = false;
  bool _disposed = false;
  String? _currentTraceId;

  Stream<AudioState> get stateStream => _stateController.stream;
  bool get isMuted => _muted;

  Future<void> playEncoded(
    Uint8List audio, {
    required String mime,
    String? traceId,
    double speed = 1,
  }) async {
    if (_muted || _disposed || audio.isEmpty) return;
    await pause(notify: false);
    _ensureBridge();
    _currentTraceId = traceId;
    _eventPoll ??= Timer.periodic(
      const Duration(milliseconds: 50),
      (_) => _readPlaybackEvent(),
    );
    final source = 'data:$mime;base64,${base64Encode(audio)}';
    final encodedTraceId = jsonEncode(traceId);
    final playbackSpeed = speed.clamp(0.8, 1.2).toDouble();
    _runJavaScript('''
      (function() {
        var bridge = document.getElementById('__qAudioDiv');
        var traceId = $encodedTraceId;
        function emit(state, message) {
          window.__qAudioState = state;
          if (state === 'failed') window.__qAudioLastError = message || '';
          var previous = bridge.textContent;
          var event = JSON.stringify({state: state, message: message || '', traceId: traceId});
          bridge.textContent = (previous && previous !== 'null' ? previous : '') + event + '\\n';
        }
        if (!window.__qAudioUnlockInstalled) {
          window.__qAudioUnlockInstalled = true;
          document.addEventListener('pointerdown', function() {
            var pending = window.__qAudioPending;
            if (!pending || !pending.__qTryPlay) return;
            window.__qAudioPending = null;
            pending.__qTryPlay();
          }, true);
        }
        if (window.__qAudio) {
          window.__qAudio.__qCancelled = true;
          if (window.__qAudioPending === window.__qAudio) window.__qAudioPending = null;
          window.__qAudio.onplay = null;
          window.__qAudio.onended = null;
          window.__qAudio.onerror = null;
          window.__qAudio.pause();
          window.__qAudio.src = '';
        }
        var player = new Audio(${jsonEncode(source)});
        player.playbackRate = $playbackSpeed;
        var terminal = false;
        var blockedReported = false;
        function finish(state, message) {
          if (terminal) return;
          terminal = true;
          if (window.__qAudioPending === player) window.__qAudioPending = null;
          emit(state, message);
        }
        function tryPlay() {
          if (terminal || player.__qCancelled) return;
          var playback = player.play();
          if (!playback) return;
          playback.catch(function(error) {
            if (error && error.name === 'NotAllowedError') {
              window.__qAudioPending = player;
              if (!blockedReported) {
                blockedReported = true;
                emit('blocked', String(error));
              }
              return;
            }
            finish('failed', String(error));
          });
        }
        window.__qAudio = player;
        player.__qTryPlay = tryPlay;
        player.onplay = function() {
          if (window.__qAudioPending === player) window.__qAudioPending = null;
          emit('started');
        };
        player.onended = function() { finish('ended'); };
        player.onerror = function() { finish('failed', 'browser audio decode failed'); };
        tryPlay();
      })();
    ''');
  }

  Future<void> pause({bool notify = true}) async {
    _runJavaScript('''
      if (window.__qAudio) {
        window.__qAudio.__qCancelled = true;
        if (window.__qAudioPending === window.__qAudio) window.__qAudioPending = null;
        window.__qAudio.onplay = null;
        window.__qAudio.onended = null;
        window.__qAudio.onerror = null;
        window.__qAudio.pause();
        window.__qAudio.src = '';
        window.__qAudio = null;
      }
    ''');
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

  void _readPlaybackEvent() {
    final bridge = web.document.getElementById('__qAudioDiv');
    final raw = bridge?.textContent;
    if (raw == null || raw.isEmpty || raw == 'null') return;
    bridge!.textContent = 'null';
    for (final line in raw.split('\n')) {
      if (line.trim().isEmpty) continue;
      _handlePlaybackEvent(line);
    }
  }

  void _handlePlaybackEvent(String raw) {
    try {
      final data = jsonDecode(raw) as Map<String, dynamic>;
      final state = data['state']?.toString();
      final eventTraceId = data['traceId']?.toString();
      if (state == 'started') {
        _emit(AudioPlaybackStatus.started, traceId: eventTraceId);
      } else if (state == 'ended') {
        if (_currentTraceId == eventTraceId) _currentTraceId = null;
        _emit(AudioPlaybackStatus.ended, traceId: eventTraceId);
      } else if (state == 'failed') {
        if (_currentTraceId == eventTraceId) _currentTraceId = null;
        _emit(
          AudioPlaybackStatus.failed,
          traceId: eventTraceId,
          error: data['message']?.toString(),
        );
      } else if (state == 'blocked') {
        _emit(
          AudioPlaybackStatus.blocked,
          traceId: eventTraceId,
          error: data['message']?.toString(),
        );
      }
    } catch (error) {
      _emit(
        AudioPlaybackStatus.failed,
        traceId: _currentTraceId,
        error: error.toString(),
      );
    }
  }

  void _ensureBridge() {
    if (web.document.getElementById('__qAudioDiv') != null) return;
    final bridge = web.HTMLDivElement()
      ..id = '__qAudioDiv'
      ..style.display = 'none'
      ..textContent = 'null';
    web.document.body?.appendChild(bridge);
  }

  void _runJavaScript(String code) {
    final script = web.HTMLScriptElement()..text = code;
    final head = web.document.head;
    if (head == null) return;
    head.appendChild(script);
    script.remove();
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
    _eventPoll?.cancel();
    await _stateController.close();
  }
}
