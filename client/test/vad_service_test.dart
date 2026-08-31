import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/vad_service.dart';

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
}
