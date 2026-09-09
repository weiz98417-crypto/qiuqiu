import 'dart:math' as math;

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/match_session_controller.dart';

void main() {
  test('controller interrupt stays below the 150ms local gate', () {
    final samples = <int>[];
    for (var index = 0; index < 100; index++) {
      final controller = MatchSessionController()
        ..beginSpeaking('trace-$index');
      final stopwatch = Stopwatch()..start();
      controller.interrupt();
      stopwatch.stop();
      samples.add(stopwatch.elapsedMicroseconds);
    }
    samples.sort();
    final p95 =
        samples[math.min(samples.length - 1, (samples.length * 95 ~/ 100))];
    expect(p95, lessThan(150000), reason: 'p95 interrupt=${p95}us');
  });

  test('fact revision projection stays below the 500ms local gate', () {
    final samples = <int>[];
    for (var index = 0; index < 100; index++) {
      final controller = MatchSessionController();
      final stopwatch = Stopwatch()..start();
      controller.dispatch(MatchFactSessionEvent('fact-$index', index + 1));
      stopwatch.stop();
      samples.add(stopwatch.elapsedMicroseconds);
    }
    samples.sort();
    final p95 =
        samples[math.min(samples.length - 1, (samples.length * 95 ~/ 100))];
    expect(p95, lessThan(500000), reason: 'p95 fact update=${p95}us');
  });
}
