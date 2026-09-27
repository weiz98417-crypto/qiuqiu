import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:web/web.dart' as web;

import 'duplex_gate.dart';
import 'turn_detector.dart';

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

@immutable
class AudioInputDevice {
  final String id;
  final String label;

  const AudioInputDevice({required this.id, required this.label});
}

void _runJavaScript(String code) {
  final script = web.HTMLScriptElement()..text = code;
  final head = web.document.head;
  if (head == null) return;
  head.appendChild(script);
  script.remove();
}

List<Map<String, dynamic>> _readRecorderEvents() {
  final bridge = web.document.getElementById('__qDiv');
  if (bridge == null) return const [];
  _runJavaScript(
    "document.getElementById('__qDiv').textContent=JSON.stringify(window.__qRecDrain?window.__qRecDrain():[])",
  );
  final raw = bridge.textContent;
  if (raw == null || raw.isEmpty || raw == 'null') return const [];
  final decoded = jsonDecode(raw);
  if (decoded is! List) return const [];
  return decoded
      .whereType<Map>()
      .map((event) => Map<String, dynamic>.from(event))
      .toList(growable: false);
}

class VADService {
  final _eventController = StreamController<VADEvent>.broadcast(sync: true);
  final _audioChunkController =
      StreamController<Uint8List>.broadcast(sync: true);
  final _inputDeviceController =
      StreamController<List<AudioInputDevice>>.broadcast(sync: true);
  final _audioBuffer = <Uint8List>[];
  Timer? _eventPoll;
  VADMode _mode = VADMode.pushToTalk;
  bool _sessionActive = false;
  bool _captureActive = false;
  bool _disposed = false;
  String _selectedInputDeviceId = '';
  String _activeInputDeviceId = '';
  List<AudioInputDevice> _inputDeviceList = const [
    AudioInputDevice(id: '', label: '系统默认麦克风'),
  ];
  Completer<List<AudioInputDevice>>? _deviceRefreshCompleter;

  // 播放期抢断门（voice-duplex 1.1 的 web 侧落形）：纯 Dart 门与原生同源，
  // 能量帧来自浏览器采集器的流式批次（JS 只在说话期吐帧）。web 的话轮收口
  // 仍在浏览器侧（sentenceSilenceMs=过渡档 1400ms），门只裁决「这句是合法
  // 抢断还是回声候选」，不改收口时机。
  final PlaybackInterruptGate _playbackGate = PlaybackInterruptGate();
  bool _playbackCaptureEnabled = true;
  // 播放期起音的公告挂起：门未过前不上报 speaking（回声不上屏、不抬相位）。
  bool _pendingGatedAnnounce = false;
  // 本句是否已过门：起音在空闲期恒过门；播放期起音要等门 fired 或播放
  // 结束，句终仍未过门的整段丢弃（回声不是话轮，AHR 原生同语义）。
  bool _utterancePassedGate = true;

  void setPlaybackCaptureEnabled(bool enabled) {
    _playbackCaptureEnabled = enabled;
  }

  void notifyPlaybackStarted() => _playbackGate.playbackStarted();

  void notifyPlaybackEnded() => _playbackGate.playbackEnded();

  /// 远程轮次模型插槽（voice-turn-detection 决策 c）：web 侧暂不接线——
  /// 话轮收口在浏览器采集器内，Dart 链无法否决或提前收口；smart-turn
  /// 真话轮实测与门校准在真机轮统一定夺后，web 再决定接线形态。
  void attachTurnModel(TurnStageDelegate? modelStage) {}

  Stream<VADEvent> get events => _eventController.stream;
  Stream<Uint8List> get audioChunks => _audioChunkController.stream;
  Stream<List<AudioInputDevice>> get inputDevices =>
      _inputDeviceController.stream;
  bool get isListening => _sessionActive;
  VADMode get mode => _mode;
  String get debugInfo => '';
  String get selectedInputDeviceId => _selectedInputDeviceId;
  String get activeInputDeviceId => _activeInputDeviceId;

  Future<bool> hasPermission() async => true;

  Future<List<AudioInputDevice>> refreshInputDevices() {
    if (_disposed) return Future.value(_inputDeviceList);
    final existing = _deviceRefreshCompleter;
    if (existing != null && !existing.isCompleted) return existing.future;
    final completer = Completer<List<AudioInputDevice>>();
    _deviceRefreshCompleter = completer;
    _ensureEventPoll();
    _runJavaScript(
      'if(window.__qRecRequestDevices)window.__qRecRequestDevices(true)',
    );
    return completer.future.timeout(
      const Duration(seconds: 4),
      onTimeout: () {
        if (identical(_deviceRefreshCompleter, completer)) {
          _deviceRefreshCompleter = null;
        }
        return _inputDeviceList;
      },
    );
  }

  Future<void> selectInputDevice(String deviceId) async {
    if (_disposed) return;
    _selectedInputDeviceId = deviceId;
    final restartCapture = _sessionActive;
    if (_captureActive) {
      _captureActive = false;
      _runJavaScript('if(window.__qRec)window.__qRec(false)');
    }
    _runJavaScript(
      'if(window.__qRecSelectDevice)window.__qRecSelectDevice(${jsonEncode(deviceId)})',
    );
    if (restartCapture) _startBrowserCapture();
  }

  Future<void> startListening(VADMode mode) async {
    if (_disposed || (_sessionActive && _captureActive && _mode == mode)) {
      return;
    }
    _mode = mode;
    _sessionActive = true;
    _audioBuffer.clear();
    _startBrowserCapture();
  }

  void _startBrowserCapture() {
    if (_disposed || !_sessionActive || _captureActive) return;
    _captureActive = true;
    _ensureEventPoll();
    _runJavaScript(
      "if(window.__qRecStart){window.__qRecStart()}else if(window.__qRecEmit){window.__qRecEmit({e:'error',message:'recorder unavailable'})}",
    );
  }

  void _ensureEventPoll() {
    _eventPoll ??= Timer.periodic(
      const Duration(milliseconds: 80),
      (_) => _checkRecorderEvent(),
    );
  }

  void _checkRecorderEvent() {
    try {
      for (final data in _readRecorderEvents()) {
        _handleRecorderEvent(data);
      }
    } catch (error) {
      _emit(VADEvent.failure(error.toString()));
    }
  }

  void _handleRecorderEvent(Map<String, dynamic> data) {
    final event = data['e'] as String?;
    final audio = data['a'] as String?;
    if (event == 'devices') {
      final devices = (data['devices'] as List?)
              ?.whereType<Map>()
              .map(
                (device) => AudioInputDevice(
                  id: device['id']?.toString() ?? '',
                  label: device['label']?.toString() ?? '麦克风',
                ),
              )
              .toList(growable: false) ??
          const <AudioInputDevice>[];
      if (devices.isNotEmpty) _inputDeviceList = devices;
      _selectedInputDeviceId = data['selectedId']?.toString() ?? '';
      final activeId = data['activeId'];
      if (activeId != null) _activeInputDeviceId = activeId.toString();
      _inputDeviceController.add(_inputDeviceList);
      final completer = _deviceRefreshCompleter;
      _deviceRefreshCompleter = null;
      if (completer != null && !completer.isCompleted) {
        completer.complete(_inputDeviceList);
      }
    } else if (event == 'deviceError') {
      final completer = _deviceRefreshCompleter;
      _deviceRefreshCompleter = null;
      if (completer != null && !completer.isCompleted) {
        completer.completeError(
          StateError(data['message']?.toString() ?? '无法读取麦克风列表'),
        );
      }
    } else if (event == 'listening') {
      _selectedInputDeviceId =
          data['selectedId']?.toString() ?? _selectedInputDeviceId;
      _activeInputDeviceId = data['deviceId']?.toString() ?? '';
      if (_sessionActive) _emit(const VADEvent.listening());
    } else if (event == 'speaking') {
      final gated = _mode == VADMode.freeTalk && _playbackCaptureEnabled;
      if (!gated || !_playbackGate.playbackActive) {
        // 空闲期起音（或未开抢断门）：照历史行为立即上报。
        _utterancePassedGate = true;
        _pendingGatedAnnounce = false;
        _emit(const VADEvent.speaking());
      } else {
        // 播放期起音：公告挂起，等门裁决（fired=合法抢断）或播放结束。
        _utterancePassedGate = false;
        _pendingGatedAnnounce = true;
      }
    } else if (event == 'audioChunk') {
      if (audio != null && audio.isNotEmpty) {
        final chunk = base64Decode(audio);
        _observePlaybackGate(chunk);
        _emitAudioChunk(chunk);
      }
    } else if (event == 'sentenceEnd') {
      _captureActive = false;
      // 段收口：JS 不流静音帧，喂一帧静音关掉门内未决段，避免下一句
      // 继承未决段的累计状态（原生靠连续帧里的静音段自然收口）。
      if (_mode == VADMode.freeTalk && _playbackCaptureEnabled) {
        _playbackGate.observe(0);
      }
      final echoCandidate = _mode == VADMode.freeTalk &&
          _playbackCaptureEnabled &&
          _playbackGate.playbackActive &&
          !_utterancePassedGate;
      if (audio != null && audio.isNotEmpty && !echoCandidate) {
        _audioBuffer.add(base64Decode(audio));
      }
      if (!echoCandidate) _emit(const VADEvent.sentenceEnd());
      _pendingGatedAnnounce = false;
      if (_mode == VADMode.pushToTalk) {
        _sessionActive = false;
      } else if (_sessionActive) {
        Timer(const Duration(milliseconds: 160), _startBrowserCapture);
      }
    } else if (event == 'permissionDenied') {
      _captureActive = false;
      _sessionActive = false;
      _emit(const VADEvent.permissionDenied());
    } else if (event == 'error') {
      _captureActive = false;
      _sessionActive = false;
      _emit(
        VADEvent.failure(data['message']?.toString() ?? 'recording failed'),
      );
    }
  }

  void stopListening() {
    if (!_sessionActive && !_captureActive) return;
    _sessionActive = false;
    _captureActive = false;
    _pendingGatedAnnounce = false;
    _runJavaScript('if(window.__qRec)window.__qRec(false)');
    _emit(const VADEvent.idle());
  }

  /// 流式批次喂抢断门：批次按 50ms 子帧切开逐帧观测（与原生帧节奏同参，
  /// 1600 字节 = 16kHz × 16bit × 50ms）。挂起的公告在门 fired 或播放结束
  /// 时放行（原生 speechStartPasses 同语义）。
  void _observePlaybackGate(Uint8List chunk) {
    if (_mode != VADMode.freeTalk || !_playbackCaptureEnabled) return;
    const frameBytes = 1600;
    for (var offset = 0; offset < chunk.length; offset += frameBytes) {
      final end =
          offset + frameBytes > chunk.length ? chunk.length : offset + frameBytes;
      final decision =
          _playbackGate.observe(pcm16Rms(Uint8List.sublistView(chunk, offset, end)));
      if (decision == PlaybackInterruptDecision.fired) {
        _utterancePassedGate = true;
        if (_pendingGatedAnnounce) {
          _pendingGatedAnnounce = false;
          _emit(const VADEvent.speaking());
        }
      } else if (_pendingGatedAnnounce && !_playbackGate.playbackActive) {
        // 播放中途结束：起音未过门但播放已停，按空闲期放行。
        _utterancePassedGate = true;
        _pendingGatedAnnounce = false;
        _emit(const VADEvent.speaking());
      }
    }
  }

  void onManualStop() {
    if (!_sessionActive) return;
    _captureActive = false;
    _runJavaScript('if(window.__qRec)window.__qRec(true)');
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
    if (_mode == VADMode.freeTalk) {
      _runJavaScript('if(window.__qRec)window.__qRec(true)');
    }
  }

  void onAudioData(Uint8List pcm) {}

  void _emit(VADEvent event) {
    if (!_disposed && !_eventController.isClosed) {
      _eventController.add(event);
    }
  }

  void _emitAudioChunk(Uint8List audio) {
    if (!_disposed && !_audioChunkController.isClosed && audio.isNotEmpty) {
      _audioChunkController.add(audio);
    }
  }

  void dispose() {
    if (_disposed) return;
    stopListening();
    _disposed = true;
    _eventPoll?.cancel();
    final completer = _deviceRefreshCompleter;
    if (completer != null && !completer.isCompleted) {
      completer.complete(_inputDeviceList);
    }
    unawaited(_eventController.close());
    unawaited(_audioChunkController.close());
    unawaited(_inputDeviceController.close());
  }
}
