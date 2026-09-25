import 'dart:async';
import 'dart:collection';
import 'dart:math';

import 'package:audio_session/audio_session.dart';
import 'package:flutter/foundation.dart';
import 'package:record/record.dart';

import 'duplex_gate.dart';
import 'turn_detector.dart';

typedef VoiceActivityDecision = ({bool detected, bool started});

const voiceRecordConfig = RecordConfig(
  encoder: AudioEncoder.pcm16bits,
  sampleRate: 16000,
  numChannels: 1,
  echoCancel: true,
  noiseSuppress: true,
);

class VoiceActivityGate {
  static const double defaultStartThreshold = 0.015;
  static const double defaultContinueThreshold = 0.006;
  static const int defaultMinimumSpeechFrames = 2;

  VoiceActivityGate({
    this.startThreshold = defaultStartThreshold,
    this.continueThreshold = defaultContinueThreshold,
    this.minimumSpeechFrames = defaultMinimumSpeechFrames,
  });

  final double startThreshold;
  final double continueThreshold;
  final int minimumSpeechFrames;
  int _speechFrames = 0;

  int get speechFrames => _speechFrames;
  bool get hasConfirmedSpeech => _speechFrames >= minimumSpeechFrames;

  VoiceActivityDecision observe(double rms) {
    final wasConfirmed = hasConfirmedSpeech;
    final threshold = wasConfirmed ? continueThreshold : startThreshold;
    final detected = rms > threshold;
    if (detected) {
      _speechFrames++;
    } else if (!wasConfirmed) {
      _speechFrames = 0;
    }
    return (
      detected: detected,
      started: !wasConfirmed && hasConfirmedSpeech,
    );
  }

  void reset() {
    _speechFrames = 0;
  }
}

/// 自动收口过门守卫（voice-duplex 1.1）：播放窗内起音且门未 fired 的段是
/// 回声候选——回声不是话轮，不得被 15s maxDuration 兜底或 turn chain 逐帧
/// 判定自动收口成回合。纯判定状态机（单测直接驱动），VADService 每帧喂
/// 起音/门状态，两条自动收口路径开跑前先问 [onsetPassed]。
class AutoFinishGuard {
  // 本段起音是否过门：空闲期起音=true；播放期起音且门未 fired=false。
  // 空闲期与按键说话恒为 true，历史行为不变。
  bool _onsetPassed = true;

  /// 本段起音是否过门：false 时 maxDuration/turn chain 两条自动收口路径
  /// 全部跳过。
  bool get onsetPassed => _onsetPassed;

  /// 逐帧喂入：起音帧按播放窗状态定段；门 fired 整段恢复过门（升级为
  /// 合法抢断话轮）。gated=false（空闲期/按键说话）恒过门。
  void observe({
    required bool speechStarted,
    required bool gated,
    required bool playbackActive,
    required bool gateFired,
  }) {
    if (gateFired) {
      _onsetPassed = true;
      return;
    }
    if (speechStarted) {
      _onsetPassed = !gated || !playbackActive;
    }
  }

  /// 话轮收口/重新开采后重置，下一段默认过门（空闲期行为）。
  void reset() {
    _onsetPassed = true;
  }
}

class VADService {
  final _eventController = StreamController<VADEvent>.broadcast(sync: true);
  final _audioChunkController =
      StreamController<Uint8List>.broadcast(sync: true);
  final _recorder = AudioRecorder();
  final _audioBuffer = <Uint8List>[];
  final _preRoll = Queue<Uint8List>();
  final _voiceActivity = VoiceActivityGate();
  // 自动收口过门守卫：播放窗内起音且门未 fired 的段（回声候选）不得被
  // maxDuration 兜底或 turn chain 逐帧判定收口成话轮。
  final AutoFinishGuard _autoFinishGuard = AutoFinishGuard();
  // 播放期抢断判定门（voice-duplex 1.1）：能量参数与空闲期同一来源。
  final PlaybackInterruptGate _playbackGate = PlaybackInterruptGate();
  // 说完判定降级链（voice-turn-detection 1.3）：模型/规则阶段预留，静默档
  // 默认固定 1400ms（评估后的过渡档，参数化可配，不硬编码在判定处）。
  // 只改 params 不换链实例，故 final。
  final TurnDetectionChain _turnChain = TurnDetectionChain();
  bool _gatedSpeechAnnounced = false;

  /// duplex_playback_capture 总开关：关闭即半双工降级，播放期不再走
  /// 抢断门，speech_start 照历史行为立即上报。
  bool _playbackCaptureEnabled = true;

  void setPlaybackCaptureEnabled(bool enabled) {
    _playbackCaptureEnabled = enabled;
  }

  /// 说完判定参数注入（voice-turn-detection 1.3）：静默阈值/策略/回退开关
  /// 全部可配置；只替换参数，不动链上已预留的模型/规则插槽。
  void configureTurnDetection(TurnDetectionParams params) {
    _turnChain.params = params;
  }

  StreamSubscription<Uint8List>? _recordSubscription;
  Timer? _silenceTimer;
  VADMode _mode = VADMode.pushToTalk;
  bool _sessionActive = false;
  bool _captureActive = false;
  bool _finishingSentence = false;
  bool _disposed = false;
  bool _audioSessionConfigured = false;
  String _selectedInputDeviceId = '';

  static const int maxDurationMs = 15000;
  static const int preRollFrames = 4;

  Stream<VADEvent> get events => _eventController.stream;
  Stream<Uint8List> get audioChunks => _audioChunkController.stream;
  Stream<List<AudioInputDevice>> get inputDevices => const Stream.empty();
  bool get isListening => _sessionActive;
  VADMode get mode => _mode;
  String get debugInfo => '';
  String get selectedInputDeviceId => _selectedInputDeviceId;
  String get activeInputDeviceId => _selectedInputDeviceId;

  Future<bool> hasPermission() => _recorder.hasPermission();

  Future<List<AudioInputDevice>> refreshInputDevices() async => const [
        AudioInputDevice(id: '', label: '系统默认麦克风'),
      ];

  Future<void> selectInputDevice(String deviceId) async {
    _selectedInputDeviceId = deviceId;
  }

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

    _voiceActivity.reset();
    _autoFinishGuard.reset();
    _preRoll.clear();
    _silenceTimer?.cancel();
    try {
      final stream = await _recorder.startStream(
        voiceRecordConfig,
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

  /// 播放起止由播放状态机回报：开始记 squash 窗锚点，结束即解除门。
  void notifyPlaybackStarted() => _playbackGate.playbackStarted();

  void notifyPlaybackEnded() => _playbackGate.playbackEnded();

  void _processAudio(Uint8List pcm) {
    if (!_sessionActive || !_captureActive || pcm.isEmpty) return;
    final rms = _calculateRms(pcm);
    final hadConfirmedSpeech = _voiceActivity.hasConfirmedSpeech;

    if (_mode == VADMode.pushToTalk) {
      _audioBuffer.add(pcm);
      _emitAudioChunk(pcm);
    } else if (!hadConfirmedSpeech) {
      _preRoll.addLast(pcm);
      while (_preRoll.length > preRollFrames) {
        _preRoll.removeFirst();
      }
    } else {
      _audioBuffer.add(pcm);
      _emitAudioChunk(pcm);
    }

    final activity = _voiceActivity.observe(rms);

    // 播放期抢断门逐帧观测（voice-duplex 1.1）：先于自动收口守卫计算决策，
    // 让「本段起音是否过门」覆盖 maxDuration 与 turn chain 两条自动路径。
    final gated = _mode == VADMode.freeTalk && _playbackCaptureEnabled;
    final decision = gated
        ? _playbackGate.observe(rms)
        : PlaybackInterruptDecision.inactive;
    if (activity.started) {
      // 起音帧定段：空闲期起音直通过门；播放期起音且门未 fired（squash
      // 压制或 armed 候选）= 回声候选段，不参与自动收口。
      _autoFinishGuard.observe(
        speechStarted: true,
        gated: gated,
        playbackActive: _playbackGate.playbackActive,
        gateFired: false,
      );
    }
    if (decision == PlaybackInterruptDecision.fired) {
      // 门 fired：本段升级为合法抢断话轮，恢复自动收口资格。
      _autoFinishGuard.observe(
        speechStarted: false,
        gated: gated,
        playbackActive: _playbackGate.playbackActive,
        gateFired: true,
      );
    }

    if (_mode == VADMode.pushToTalk) {
      _audioBuffer.add(pcm);
      _emitAudioChunk(pcm);
    } else if (!hadConfirmedSpeech) {
      _preRoll.addLast(pcm);
      while (_preRoll.length > preRollFrames) {
        _preRoll.removeFirst();
      }
    } else {
      _audioBuffer.add(pcm);
      _emitAudioChunk(pcm);
    }

    if (activity.detected) {
      _silenceTimer?.cancel();
      _silenceTimer = null;
      if (activity.started) {
        if (_mode == VADMode.freeTalk) {
          _audioBuffer.addAll(_preRoll);
          for (final chunk in _preRoll) {
            _emitAudioChunk(chunk);
          }
          _preRoll.clear();
        }
      }
      // 15s 兜底自动收口：起音未过门的段（播放期回声候选）跳过——回声
      // 不是话轮，不得被时长兜底提交成回合。
      if (_autoFinishGuard.onsetPassed &&
          _voiceActivity.speechFrames >= maxDurationMs ~/ 50) {
        unawaited(_finishSentence());
      }
    } else if (_voiceActivity.hasConfirmedSpeech) {
      // 静默兜底定时器：时长取降级链当前生效阈值（voice-turn-detection 1.3，
      // 参数化不硬编码）；正常路径由链逐帧判定先行收口。
      _silenceTimer ??= Timer(
        _turnChain.effectiveSilenceThreshold,
        () => unawaited(_finishSentence()),
      );
    }

    // 说完判定降级链（voice-turn-detection 1.3）：逐帧喂 RMS，链判完即收口。
    // 覆盖包间隔不齐时定时器漏计的边缘，判完点以链的逐帧结论为准。起音
    // 未过门的段（播放期回声候选）不观测——回声帧不得驱动话轮判定。
    if (_mode == VADMode.freeTalk &&
        !_turnChain.decided &&
        _autoFinishGuard.onsetPassed &&
        _voiceActivity.hasConfirmedSpeech) {
      final turn = _turnChain.observe(rms);
      if (turn.decided) {
        unawaited(_finishSentence());
      }
    }

    // 空闲期 speech_start 照常立即上报；播放期须过 squash 窗、能量门与
    // 时长门，门拒绝时保持静默——播放与 ASR 采集都不受影响（收音本来就
    // 在传）。总开关关闭或按键说话（显式意图）不走门。
    if (decision == PlaybackInterruptDecision.rejected ||
        decision == PlaybackInterruptDecision.inactive) {
      _gatedSpeechAnnounced = false;
    }
    final speechStartPasses = !gated || !_playbackGate.playbackActive;
    final shouldAnnounce = (activity.started && speechStartPasses) ||
        (decision == PlaybackInterruptDecision.fired && !_gatedSpeechAnnounced);
    if (shouldAnnounce) {
      _gatedSpeechAnnounced = true;
      _emit(const VADEvent.speaking());
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

    final hasSpeech = _voiceActivity.hasConfirmedSpeech;
    if (hasSpeech || _mode == VADMode.pushToTalk) {
      _emit(const VADEvent.sentenceEnd());
    }
    // 话轮收口：判定链重置，等待下一个话轮（voice-turn-detection 1.3）。
    _turnChain.reset();

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
    _voiceActivity.reset();
    _autoFinishGuard.reset();
    _turnChain.reset();
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

  void _emitAudioChunk(Uint8List audio) {
    if (!_disposed && !_audioChunkController.isClosed && audio.isNotEmpty) {
      _audioChunkController.add(audio);
    }
  }

  void dispose() {
    if (_disposed) return;
    stopListening();
    _disposed = true;
    unawaited(_eventController.close());
    unawaited(_audioChunkController.close());
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

@immutable
class AudioInputDevice {
  final String id;
  final String label;

  const AudioInputDevice({required this.id, required this.label});
}
