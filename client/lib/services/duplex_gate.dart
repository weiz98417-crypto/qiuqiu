import 'vad_service.dart';

/// 播放期抢断门参数。能量门默认 = 空闲期 start 阈值 × 倍率；时长门挡住
/// 喷麦与短促环境音；squash 窗吸收播放起音的爆破音与耳机漏音。
class PlaybackGateParams {
  /// 空闲期 start 阈值（RMS），直接取 [VoiceActivityGate.defaultStartThreshold]，
  /// 单一来源，不另设常量。
  final double idleStartThreshold;

  /// 播放期置信门倍率：能量门 = idleStartThreshold × confidenceFactor。
  final double confidenceFactor;

  /// 抢断所需的最短持续语音时长。
  final Duration minSpeechDuration;

  /// 播放开始后该窗口内的起音整段忽略。
  final Duration squashWindow;

  /// 单帧时长，默认对齐 16kHz 采集的 50ms 帧节奏。
  final Duration frameDuration;

  const PlaybackGateParams({
    this.idleStartThreshold = VoiceActivityGate.defaultStartThreshold,
    this.confidenceFactor = 1.5,
    this.minSpeechDuration = const Duration(milliseconds: 400),
    this.squashWindow = const Duration(milliseconds: 300),
    this.frameDuration = const Duration(milliseconds: 50),
  });

  /// 播放期能量门（绝对 RMS）：低于它的帧不累计语音时长。
  double get confidenceGate => idleStartThreshold * confidenceFactor;
}

/// 播放期抢断决策：门只在音频播放期间生效，逐帧产出。
enum PlaybackInterruptDecision {
  /// 门未生效（无播放）或当前无候选段。
  inactive,

  /// 起音落在 squash 窗内，整段忽略（回声主防线）。
  suppressed,

  /// 候选段累计中，尚未过时长门。
  armed,

  /// 过全部判定门——客户端据此抢断（段内锁存，只触发一次）。
  fired,

  /// 候选段因能量跌破 continue 阈值结束，未过时长门。
  rejected,
}

/// 播放期抢断判定门（voice-duplex 1.1）。
///
/// 纯判定：输入逐帧 RMS 与播放起止事件，输出逐帧决策，不碰时钟、不碰
/// 采集。语义：
/// - 空闲期（无播放）门直通，VAD 行为与历史一致；
/// - 播放期起音先过 squash 窗，再以播放期能量门计持续语音帧，连续满
///   [PlaybackGateParams.minSpeechDuration] 才放行抢断；
/// - 段以能量跌破空闲期 continue 阈值收口，收口后 squash/时长判定全部
///   重新计。
class PlaybackInterruptGate {
  PlaybackGateParams params;

  bool _playbackActive = false;
  Duration _playbackElapsed = Duration.zero;
  bool _suppressedSegment = false;
  bool _firedSegment = false;
  bool _candidateArmed = false;
  int _speechFrames = 0;

  PlaybackInterruptGate({this.params = const PlaybackGateParams()});

  bool get playbackActive => _playbackActive;

  /// 当前候选段已累计的持续语音时长（测试/校准观测用）。
  Duration get speechDuration =>
      params.frameDuration * _speechFrames;

  void playbackStarted() {
    _playbackActive = true;
    _playbackElapsed = Duration.zero;
    _resetSegment();
  }

  void playbackEnded() {
    _playbackActive = false;
    _resetSegment();
  }

  /// 逐帧观测：返回该帧后的抢断决策。
  PlaybackInterruptDecision observe(double rms) {
    if (!_playbackActive) {
      _resetSegment();
      return PlaybackInterruptDecision.inactive;
    }
    _playbackElapsed += params.frameDuration;
    // 段收口：以空闲期 continue 阈值为界，与之下的能量视为一段语音结束。
    if (rms < VoiceActivityGate.defaultContinueThreshold) {
      final hadSegment =
          _suppressedSegment || _firedSegment || _candidateArmed;
      _resetSegment();
      return hadSegment
          ? PlaybackInterruptDecision.rejected
          : PlaybackInterruptDecision.inactive;
    }
    final loud = rms >= params.confidenceGate;
    if (!_candidateArmed && !_suppressedSegment) {
      if (!loud) {
        // 高于 continue 但低于播放期能量门：透明帧，不构成候选。
        return PlaybackInterruptDecision.inactive;
      }
      // 段起点：先过 squash 窗，窗口内的起音整段忽略。
      if (_playbackElapsed < params.squashWindow) {
        _suppressedSegment = true;
        return PlaybackInterruptDecision.suppressed;
      }
      _candidateArmed = true;
      _speechFrames = 1;
      return params.frameDuration * _speechFrames >= params.minSpeechDuration
          ? _fire()
          : PlaybackInterruptDecision.armed;
    }
    if (_suppressedSegment) {
      // 已被 squash 的段保持忽略，直到段收口。
      return PlaybackInterruptDecision.suppressed;
    }
    if (!loud) {
      // 段内能量回落到门下：持续时长重新计（宁保守勿误断）。
      _speechFrames = 0;
      return PlaybackInterruptDecision.armed;
    }
    if (_firedSegment) {
      return PlaybackInterruptDecision.fired;
    }
    _speechFrames++;
    return params.frameDuration * _speechFrames >= params.minSpeechDuration
        ? _fire()
        : PlaybackInterruptDecision.armed;
  }

  PlaybackInterruptDecision _fire() {
    _firedSegment = true;
    return PlaybackInterruptDecision.fired;
  }

  void _resetSegment() {
    _suppressedSegment = false;
    _firedSegment = false;
    _candidateArmed = false;
    _speechFrames = 0;
  }

  void reset() {
    _playbackActive = false;
    _playbackElapsed = Duration.zero;
    _resetSegment();
  }
}

/// 纯判定入口：给定播放状态与一段逐帧 RMS 序列，输出逐帧决策。
/// 校准与测试用它锁行为，无需触碰采集栈。
List<PlaybackInterruptDecision> evaluatePlaybackInterrupt(
  List<double> rmsFrames, {
  required bool playbackActive,
  PlaybackGateParams params = const PlaybackGateParams(),
}) {
  final gate = PlaybackInterruptGate(params: params);
  if (playbackActive) gate.playbackStarted();
  return [
    for (final rms in rmsFrames) gate.observe(rms),
  ];
}
