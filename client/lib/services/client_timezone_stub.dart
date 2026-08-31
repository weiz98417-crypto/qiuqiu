String clientTimezone() => offsetTimezone(DateTime.now().timeZoneOffset);

String offsetTimezone(Duration offset) {
  final totalMinutes = offset.inMinutes;
  final sign = totalMinutes < 0 ? '-' : '+';
  final absoluteMinutes = totalMinutes.abs();
  final hours = (absoluteMinutes ~/ 60).toString().padLeft(2, '0');
  final minutes = (absoluteMinutes % 60).toString().padLeft(2, '0');
  return 'UTC$sign$hours:$minutes';
}
