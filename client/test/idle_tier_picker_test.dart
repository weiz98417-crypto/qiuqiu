import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/idle_tier_picker.dart';

void main() {
  DateTime at(int seconds) => DateTime.utc(2026, 9, 16, 20, 0, seconds);

  test('affect thresholds map to the three idle tiers', () {
    expect(
      IdleTierPicker.tierFor(valence: -0.6, arousal: 0.2),
      IdleTier.deflated,
    );
    expect(
      IdleTierPicker.tierFor(valence: 0, arousal: 0.1),
      IdleTier.deflated,
    );
    expect(IdleTierPicker.tierFor(valence: 0, arousal: 0.2), IdleTier.calm);
    expect(
      IdleTierPicker.tierFor(valence: 0.4, arousal: 0.7),
      IdleTier.energetic,
    );
  });

  test('tiers pick among the three idle motions', () {
    expect(IdleTierPicker.motionFor(IdleTier.deflated), 'idle_01');
    expect(IdleTierPicker.motionFor(IdleTier.calm), 'idle_02');
    expect(IdleTierPicker.motionFor(IdleTier.energetic), 'idle_03');
  });

  test('re-picks only when the interval elapsed', () {
    final picker = IdleTierPicker();
    expect(picker.maybeRepick(at(0)), 'idle_02'); // neutral affect → calm tier
    expect(picker.maybeRepick(at(10)), isNull);
    expect(picker.maybeRepick(at(30)), 'idle_02');
  });

  test('affect updates steer the tier on the next re-pick', () {
    final picker = IdleTierPicker();
    picker.updateAffect(valence: -0.8, arousal: 0.2);
    expect(picker.maybeRepick(at(0)), 'idle_01');
    picker.updateAffect(valence: 0.6, arousal: 0.8);
    expect(picker.maybeRepick(at(30)), 'idle_03');
  });

  test('tier switches honour the hysteresis lock', () {
    final picker = IdleTierPicker();
    picker.updateAffect(valence: 0.6, arousal: 0.8);
    expect(picker.maybeRepick(at(0)), 'idle_02'); // calm default
    expect(picker.maybeRepick(at(30)), 'idle_03'); // first switch
    picker.updateAffect(valence: -0.4, arousal: 0.2);
    expect(picker.maybeRepick(at(60)), 'idle_03'); // inside the 60s lock
    expect(picker.maybeRepick(at(90)), 'idle_01'); // lock expired
  });

  test('a clear mood swing switches tier immediately', () {
    final picker = IdleTierPicker();
    expect(picker.maybeRepick(at(0)), 'idle_02'); // calm default
    picker.updateAffect(valence: 0.8, arousal: 0.95);
    expect(picker.maybeRepick(at(30)), 'idle_03'); // first switch
    picker.updateAffect(valence: -1.0, arousal: 0.0);
    expect(picker.maybeRepick(at(60)), 'idle_01'); // margin crossed inside lock
  });
}
