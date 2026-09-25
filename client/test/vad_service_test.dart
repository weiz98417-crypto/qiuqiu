import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/duplex_gate.dart';
import 'package:qiuqiu/services/turn_detector.dart';
import 'package:qiuqiu/services/vad_service.dart';

/// 复现 VADService._processAudio 的自动收口管线（过门守卫 + maxDuration
/// 兜底 + turn chain 判定），喂帧后断言回声段不被收口成话轮。管线与实现
/// 同构：两条自动收口路径开跑前都先问 AutoFinishGuard.onsetPassed。
class _AutoFinishPipeline {
  final VoiceActivityGate vad = VoiceActivityGate();
  final PlaybackInterruptGate gate = PlaybackInterruptGate();
  final AutoFinishGuard guard = AutoFinishGuard();
  final TurnDetectionChain chain = TurnDetectionChain();
  bool finished = false;

  _AutoFinishPipeline({required bool playback}) {
    if (playback) gate.playbackStarted();
  }

  void feed(double rms) {
    final activity = vad.observe(rms);
    final decision = gate.observe(rms);
    guard.observe(
      speechStarted: activity.started,
      gated: true,
      playbackActive: gate.playbackActive,
      gateFired: decision == PlaybackInterruptDecision.fired,
    );
    if (guard.onsetPassed && vad.speechFrames >= VADService.maxDurationMs ~/ 50) {
      finished = true; // maxDuration 兜底
    }
    if (!chain.decided && guard.onsetPassed && vad.hasConfirmedSpeech) {
      if (chain.observe(rms).decided) {
        finished = true; // turn chain 判完
      }
    }
  }
}

void main() {
  test('voice capture requests acoustic echo cancellation', () {
    expect(voiceRecordConfig.echoCancel, isTrue);
    expect(voiceRecordConfig.noiseSuppress, isTrue);
  });

  test('separated outdoor noise spikes cannot accumulate into speech', () {
    final gate = VoiceActivityGate();

    expect(gate.observe(0.016).started, isFalse);
    expect(gate.observe(0.012).detected, isFalse);
    expect(gate.observe(0.016).started, isFalse);
    expect(gate.hasConfirmedSpeech, isFalse);

    expect(gate.observe(0.016).started, isTrue);
    expect(gate.hasConfirmedSpeech, isTrue);
  });

  test('quiet speech continues after the higher start threshold', () {
    final gate = VoiceActivityGate();

    gate.observe(0.016);
    expect(gate.observe(0.016).started, isTrue);
    expect(gate.observe(0.010).detected, isTrue);
    expect(gate.hasConfirmedSpeech, isTrue);
  });

  test('播放窗内歧义带回声帧序列不被自动收口成话轮', () {
    final pipeline = _AutoFinishPipeline(playback: true);
    // 0.018 高于空闲 start 阈值（VAD 确认语音）但低于播放期能量门
    // （0.015×1.5=0.0225，门透明帧不 fired）——speech_start 被门挡住，
    // 320 帧（约 16s）远超 15s maxDuration 也不得触发任何自动收口。
    for (var i = 0; i < 320; i++) {
      pipeline.feed(0.018);
    }
    expect(pipeline.finished, isFalse);
    expect(pipeline.guard.onsetPassed, isFalse);
    expect(pipeline.chain.speechConfirmed, isFalse);
  });

  test('门 fired 抢断后回声守卫放行，话轮照常判完', () {
    final pipeline = _AutoFinishPipeline(playback: true);
    for (var i = 0; i < 20; i++) {
      pipeline.feed(0.018); // 回声段：守卫拦截，链不观测
    }
    expect(pipeline.guard.onsetPassed, isFalse);
    for (var i = 0; i < 10; i++) {
      pipeline.feed(0.03); // 用户真抢断：响帧凑满时长门 → fired
    }
    expect(pipeline.guard.onsetPassed, isTrue);
    for (var i = 0; i < 30; i++) {
      pipeline.feed(0.001); // 说完静默：链正常判完
    }
    expect(pipeline.chain.decided, isTrue);
    expect(pipeline.finished, isTrue);
  });

  test('空闲期起音不受守卫影响：15s 兜底照常收口', () {
    final pipeline = _AutoFinishPipeline(playback: false);
    for (var i = 0; i < 300; i++) {
      pipeline.feed(0.03);
    }
    expect(pipeline.guard.onsetPassed, isTrue);
    expect(pipeline.finished, isTrue);
  });
}
