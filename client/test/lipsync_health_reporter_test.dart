import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/lipsync_health_reporter.dart';

void main() {
  test('同一原因在冷却窗口内只报一次，过期后放行', () {
    var at = DateTime(2026, 10, 3, 12);
    final reporter = LipSyncHealthReporter(
      cooldown: const Duration(minutes: 10),
      now: () => at,
    );

    expect(reporter.shouldReport('jitter_fallback'), isTrue);
    at = at.add(const Duration(minutes: 9));
    expect(reporter.shouldReport('jitter_fallback'), isFalse);
    at = at.add(const Duration(minutes: 10));
    expect(reporter.shouldReport('jitter_fallback'), isTrue);
  });

  test('不同原因互不挤占冷却窗口', () {
    final reporter = LipSyncHealthReporter(now: () => DateTime(2026, 10, 3));
    expect(reporter.shouldReport('vendor_unavailable'), isTrue);
    expect(reporter.shouldReport('decode_failed'), isTrue);
    expect(reporter.shouldReport('vendor_unavailable'), isFalse);
  });
}
