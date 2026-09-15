import 'package:web/web.dart' as web;

import 'client_timezone_stub.dart' show offsetTimezone;

String clientTimezone() {
  final timezone =
      web.document.getElementById('__qTimezone')?.textContent?.trim() ?? '';
  if (timezone.isNotEmpty) return timezone;
  return offsetTimezone(DateTime.now().timeZoneOffset);
}
