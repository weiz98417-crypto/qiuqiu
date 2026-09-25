import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/duplex_gate.dart';
import 'package:qiuqiu/services/vad_service.dart';

void main() {
  // 默认帧 50ms：时长门 400ms = 8 个响帧；squash 窗 300ms 覆盖前 5 帧。
  const params = PlaybackGateParams();

  test('空闲期（无播放）门直通，逐帧 inactive', () {
    final decisions = evaluatePlaybackInterrupt(
      [0.03, 0.001, 0.5, 0.001],
      playbackActive: false,
    );
    expect(decisions, everyElement(PlaybackInterruptDecision.inactive));
  });

  test('抢断正例：过 squash 窗后持续 400ms 的响语音触发 fired', () {
    // 前 7 帧（350ms）安静，起音落在 squash 窗之外；8 个响帧凑满 400ms。
    final decisions = evaluatePlaybackInterrupt(
      [0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.001] +
          List.filled(9, 0.03),
      playbackActive: true,
    );
    expect(
      decisions.take(7),
      everyElement(PlaybackInterruptDecision.inactive),
    );
    expect(decisions[7], PlaybackInterruptDecision.armed);
    expect(decisions[13], PlaybackInterruptDecision.armed);
    expect(decisions[14], PlaybackInterruptDecision.fired);
    // fired 段内锁存：后续响帧不重复触发。
    expect(decisions[15], PlaybackInterruptDecision.fired);
  });

  test('回声负例：播放起音即响整段落在 squash 窗内被忽略', () {
    final decisions = evaluatePlaybackInterrupt(
      [0.001] + List.filled(20, 0.03),
      playbackActive: true,
    );
    expect(decisions.first, PlaybackInterruptDecision.inactive);
    expect(
      decisions.skip(1),
      everyElement(PlaybackInterruptDecision.suppressed),
    );
    expect(decisions, isNot(contains(PlaybackInterruptDecision.fired)));
  });

  test('回声负例：不足 400ms 的短促响音被时长门拒绝', () {
    final decisions = evaluatePlaybackInterrupt(
      [0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.03, 0.03, 0.03,
        0.001],
      playbackActive: true,
    );
    expect(
      decisions.sublist(7, 10),
      everyElement(PlaybackInterruptDecision.armed),
    );
    expect(decisions.last, PlaybackInterruptDecision.rejected);
    expect(decisions, isNot(contains(PlaybackInterruptDecision.fired)));
  });

  test('回声负例：低于播放期能量门的持续声响不构成候选', () {
    // 0.018 高于空闲 continue 阈值但低于播放期门（0.015×1.5=0.0225）。
    final decisions = evaluatePlaybackInterrupt(
      [0.001, 0.001, 0.001, 0.001] + List.filled(10, 0.018),
      playbackActive: true,
    );
    expect(
      decisions.skip(4),
      everyElement(PlaybackInterruptDecision.inactive),
    );
    expect(decisions, isNot(contains(PlaybackInterruptDecision.fired)));
  });

  test('段内能量回落重新累计持续时长，凑不满就不触发', () {
    final decisions = evaluatePlaybackInterrupt(
      [0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.001] +
          [0.03, 0.03, 0.03, 0.03] +
          [0.010, 0.010] +
          [0.03, 0.03, 0.03, 0.03] +
          [0.001],
      playbackActive: true,
    );
    expect(decisions, isNot(contains(PlaybackInterruptDecision.fired)));
    expect(decisions.last, PlaybackInterruptDecision.rejected);
  });

  test('fired 段内锁存：歧义带帧不回退 armed、不清零持续时长', () {
    // 7 帧安静 + 8 帧响（凑满 400ms 触发 fired）+ 2 帧歧义带（0.018，
    // 高于 continue 阈值低于播放期能量门）——fired 后决策保持 fired。
    final decisions = evaluatePlaybackInterrupt(
      [0.001, 0.001, 0.001, 0.001, 0.001, 0.001, 0.001] +
          List.filled(8, 0.03) +
          [0.018, 0.018],
      playbackActive: true,
    );
    expect(decisions[14], PlaybackInterruptDecision.fired);
    expect(decisions[15], PlaybackInterruptDecision.fired);
    expect(decisions[16], PlaybackInterruptDecision.fired);
  });

  test('播放结束解除门，再次播放重新进入 squash 窗', () {
    final gate = PlaybackInterruptGate(params: params);
    gate.playbackStarted();
    expect(gate.observe(0.001), PlaybackInterruptDecision.inactive);
    expect(gate.observe(0.03), PlaybackInterruptDecision.suppressed);
    gate.playbackEnded();
    expect(gate.playbackActive, isFalse);
    expect(gate.observe(0.03), PlaybackInterruptDecision.inactive);

    gate.playbackStarted();
    expect(gate.playbackActive, isTrue);
    expect(gate.observe(0.03), PlaybackInterruptDecision.suppressed);
  });

  test('能量门默认 = 空闲期 start 阈值 × 1.5，单一来源不漂移', () {
    expect(params.idleStartThreshold,
        VoiceActivityGate.defaultStartThreshold);
    expect(
      params.confidenceGate,
      closeTo(VoiceActivityGate.defaultStartThreshold * 1.5, 1e-12),
    );
    expect(params.minSpeechDuration, const Duration(milliseconds: 400));
    expect(params.squashWindow, const Duration(milliseconds: 300));
  });
}
