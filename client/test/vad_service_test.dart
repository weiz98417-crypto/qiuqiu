import 'dart:typed_data';

import 'package:flutter/services.dart';
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
  TestWidgetsFlutterBinding.ensureInitialized();

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

  test('静默定时器不收口播放期未过门的回声段', () async {
    final states = await _runSilenceTimerScenario((vad, states) async {
      vad.notifyPlaybackStarted();
      for (var i = 0; i < 40; i++) {
        vad.onAudioData(_pcmFrame(0.018)); // 歧义带回声：起音未过门
      }
      vad.notifyPlaybackEnded();
      for (var i = 0; i < 5; i++) {
        vad.onAudioData(_pcmFrame(0.001)); // 回声停：静默帧挂起定时器
      }
    });
    // hasConfirmedSpeech 锁存曾让定时器把回声段收口成话轮——守卫必须拦下。
    expect(states, isNot(contains(VADState.sentenceEnd)));
    expect(states, contains(VADState.listening)); // 采集仍在，未被收口
  });

  test('门 fired 段静默定时器照常收口话轮', () async {
    final states = await _runSilenceTimerScenario((vad, states) async {
      vad.notifyPlaybackStarted();
      for (var i = 0; i < 40; i++) {
        vad.onAudioData(_pcmFrame(0.018)); // 回声段：守卫拦截
      }
      for (var i = 0; i < 12; i++) {
        vad.onAudioData(_pcmFrame(0.03)); // 真抢断：响帧凑满时长门 → fired
      }
      vad.notifyPlaybackEnded();
      for (var i = 0; i < 5; i++) {
        vad.onAudioData(_pcmFrame(0.001)); // 说完静默：定时器收口
      }
    });
    expect(states, contains(VADState.sentenceEnd));
  });

  test('空闲期静默定时器照常收口话轮', () async {
    final states = await _runSilenceTimerScenario((vad, states) async {
      for (var i = 0; i < 10; i++) {
        vad.onAudioData(_pcmFrame(0.03)); // 空闲期起音：恒过门
      }
      for (var i = 0; i < 5; i++) {
        vad.onAudioData(_pcmFrame(0.001));
      }
    });
    expect(states, contains(VADState.sentenceEnd));
  });
}

/// record 插件平台通道桩：驱动真实 VADService 时把平台调用拦在测试进程内，
/// 权限恒通过、起流/停流为空操作。音频帧经 onAudioData 直接喂入，不依赖
/// 事件通道回放；事件通道按名注册空桩（recorderId 取自 create 参数）。
class _RecordPlatformStub {
  _RecordPlatformStub() {
    _messenger.setMockMethodCallHandler(
      const MethodChannel('com.llfbandit.record/messages'),
      _onMethodCall,
    );
  }

  TestDefaultBinaryMessenger get _messenger =>
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;

  Future<Object?> _onMethodCall(MethodCall call) async {
    if (call.method == 'create') {
      final recorderId = call.arguments['recorderId'] as String;
      for (final prefix in const ['events', 'eventsRecord']) {
        _messenger.setMockMethodCallHandler(
          MethodChannel('com.llfbandit.record/$prefix/$recorderId'),
          (_) async => null,
        );
      }
    }
    if (call.method == 'hasPermission') return true;
    return null;
  }
}

/// 50ms@16kHz 单帧 PCM（800 个 16bit 采样），恒定幅值近似目标 RMS；
/// VAD 以「一次 onAudioData 调用 = 一帧」计数，字节数不影响判定节奏。
Uint8List _pcmFrame(double rms) {
  final amplitude = ((rms.clamp(0.0, 1.0)) * 32767).round();
  final bytes = Uint8List(1600);
  final view = ByteData.view(bytes.buffer);
  for (var offset = 0; offset < bytes.length; offset += 2) {
    view.setInt16(offset, amplitude, Endian.little);
  }
  return bytes;
}

/// 静默定时器路径（`_silenceTimer → _finishSentence`）的真实服务测试：
/// 判定链关回退（tuned 且无兜底）后链悬而不决，静默定时器成为唯一自动
/// 收口路径——守卫是否覆盖定时器触发由此可单独观测。
Future<List<VADState>> _runSilenceTimerScenario(
  Future<void> Function(VADService vad, List<VADState> states) scenario,
) async {
  _RecordPlatformStub();
  final vad = VADService();
  addTearDown(vad.dispose);
  vad.configureTurnDetection(const TurnDetectionParams(
    strategy: TurnSilenceStrategy.tuned,
    tunedThreshold: Duration(milliseconds: 20),
    fallbackEnabled: false,
  ));
  final states = <VADState>[];
  vad.events.listen((event) => states.add(event.state));
  await vad.startListening(VADMode.freeTalk);
  await scenario(vad, states);
  // 静默定时器 20ms + 收口后 160ms 重开采，留足真实时钟余量。
  await Future<void>.delayed(const Duration(milliseconds: 250));
  return states;
}
