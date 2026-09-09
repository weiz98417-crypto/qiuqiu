import 'package:flutter/foundation.dart';

@immutable
class MatchViewData {
  final String homeTeam;
  final String awayTeam;
  final int homeScore;
  final int awayScore;
  final String competition;
  final String period;
  final String clock;
  final List<String> recentEventLabels;
  final bool hasMatchInfo;

  const MatchViewData({
    this.homeTeam = '主队',
    this.awayTeam = '客队',
    this.homeScore = 0,
    this.awayScore = 0,
    this.competition = '',
    this.period = '',
    this.clock = '',
    this.recentEventLabels = const [],
    this.hasMatchInfo = false,
  });

  String get liveLabel => switch (period.trim().toLowerCase()) {
        '' || 'pre_match' => '等待开赛',
        'finished' || 'full_time' || 'fulltime' => '已结束',
        _ => '直播中',
      };

  String get eventLabel => statusCarouselItems.first;

  List<String> get statusCarouselItems {
    if (!hasMatchInfo) return const ['等待比赛信息'];
    final items = <String>[];
    for (final event in recentEventLabels) {
      final normalized = event.trim();
      if (normalized.isNotEmpty && !items.contains(normalized)) {
        items.add(normalized);
      }
    }
    final phaseLabel = _displayPeriod(period);
    final scoreLabel = '$homeTeam $homeScore—$awayScore $awayTeam';
    final currentState = '$phaseLabel · $scoreLabel';
    if (!items.contains(currentState)) items.add(currentState);
    final clockLabel = clock.trim();
    if (clockLabel.isNotEmpty) {
      final timing = [
        clockLabel,
        if (competition.trim().isNotEmpty) competition.trim(),
        liveLabel,
      ].join(' · ');
      if (!items.contains(timing)) items.add(timing);
    }
    return List.unmodifiable(items);
  }

  String get statusCarouselContentRevision {
    final items = statusCarouselItems.toList(growable: false);
    if (clock.trim().isEmpty || items.isEmpty) {
      return items.join('\u001f');
    }
    final stableItems = items.toList();
    stableItems[stableItems.length - 1] = [
      'live-clock',
      competition.trim(),
      liveLabel,
    ].join('\u001e');
    return stableItems.join('\u001f');
  }

  MatchViewData withSnapshot(Map<String, dynamic> snapshot, {DateTime? now}) {
    final score = _map(snapshot['score']);
    final matchClock =
        MatchClockViewData.tryParse(_map(snapshot['matchClock']));
    final events = snapshot['recentEvents'];
    final eventLabels = events is List
        ? events
            .map(_map)
            .whereType<Map<String, dynamic>>()
            .map(_eventDescription)
            .take(4)
            .toList(growable: false)
        : const <String>[];
    return copyWith(
      homeTeam: snapshot['homeTeam'] as String?,
      awayTeam: snapshot['awayTeam'] as String?,
      homeScore: _integer(score?['home']),
      awayScore: _integer(score?['away']),
      period: matchClock?.period ?? snapshot['period'] as String?,
      clock: matchClock?.displayAt((now ?? DateTime.now()).toUtc()) ??
          snapshot['clock'] as String?,
      recentEventLabels: eventLabels,
      hasMatchInfo: true,
    );
  }

  MatchViewData withEvent(Map<String, dynamic> event) {
    final score = _map(event['score']);
    final eventLabel = _eventDescription(event);
    final eventLabels = [
      eventLabel,
      ...recentEventLabels.where((item) => item != eventLabel),
    ].take(4).toList(growable: false);
    return copyWith(
      homeScore: _integer(score?['home']),
      awayScore: _integer(score?['away']),
      recentEventLabels: eventLabels,
      hasMatchInfo: true,
    );
  }

  MatchViewData withLegacyScore(String score) {
    final parts = score.split(RegExp(r'[-—:]'));
    if (parts.length != 2) return this;
    return copyWith(
      homeScore: int.tryParse(parts.first.trim()),
      awayScore: int.tryParse(parts.last.trim()),
    );
  }

  MatchViewData copyWith({
    String? homeTeam,
    String? awayTeam,
    int? homeScore,
    int? awayScore,
    String? competition,
    String? period,
    String? clock,
    List<String>? recentEventLabels,
    bool? hasMatchInfo,
  }) {
    return MatchViewData(
      homeTeam: homeTeam?.trim().isNotEmpty == true ? homeTeam! : this.homeTeam,
      awayTeam: awayTeam?.trim().isNotEmpty == true ? awayTeam! : this.awayTeam,
      homeScore: homeScore ?? this.homeScore,
      awayScore: awayScore ?? this.awayScore,
      competition: competition ?? this.competition,
      period: period?.trim().isNotEmpty == true ? period! : this.period,
      clock: clock?.trim().isNotEmpty == true ? clock! : this.clock,
      recentEventLabels: recentEventLabels ?? this.recentEventLabels,
      hasMatchInfo: hasMatchInfo ?? this.hasMatchInfo,
    );
  }
}

@immutable
class MatchClockViewData {
  final String period;
  final int elapsedSeconds;
  final bool running;
  final DateTime? anchorAt;
  final int version;

  const MatchClockViewData({
    required this.period,
    required this.elapsedSeconds,
    required this.running,
    required this.anchorAt,
    required this.version,
  });

  static MatchClockViewData? tryParse(Map<String, dynamic>? value) {
    if (value == null) return null;
    final anchorRaw = value['anchorAt']?.toString();
    return MatchClockViewData(
      period: value['period']?.toString().trim().isNotEmpty == true
          ? value['period'].toString()
          : 'pre_match',
      elapsedSeconds: _integer(value['elapsedSeconds']) ?? 0,
      running: value['running'] == true,
      anchorAt:
          anchorRaw == null ? null : DateTime.tryParse(anchorRaw)?.toUtc(),
      version: _integer(value['version']) ?? 0,
    );
  }

  int elapsedAt(DateTime now) {
    var elapsed = elapsedSeconds;
    if (running && anchorAt != null) {
      final delta = now.toUtc().difference(anchorAt!).inSeconds;
      if (delta > 0) elapsed += delta;
    }
    return elapsed < 0 ? 0 : elapsed;
  }

  String displayAt(DateTime now) {
    final elapsed = elapsedAt(now);
    final minutes = (elapsed ~/ 60).toString().padLeft(2, '0');
    final seconds = (elapsed % 60).toString().padLeft(2, '0');
    return '$minutes:$seconds';
  }
}

Map<String, dynamic>? _map(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return value.cast<String, dynamic>();
  return null;
}

int? _integer(dynamic value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '');
}

String _displayPeriod(String value) {
  return switch (value.trim().toLowerCase()) {
    '' || 'pre_match' => '赛前',
    'first_half' => '上半场',
    'half_time' || 'halftime' => '中场休息',
    'second_half' => '下半场',
    'extra_time' => '加时赛',
    'penalties' => '点球大战',
    'finished' || 'full_time' || 'fulltime' => '全场结束',
    _ => value.trim(),
  };
}

String _eventDescription(Map<String, dynamic> event) {
  final eventClock = event['clock']?.toString().trim();
  final timing = eventClock?.isNotEmpty == true ? eventClock! : '刚刚';
  final description = event['description'] as String?;
  if (description != null && description.trim().isNotEmpty) {
    return '$timing · ${description.trim()}';
  }
  final player = event['playerName'] as String?;
  final eventType = event['eventType'] as String? ?? '';
  final label = switch (eventType) {
    'goal' => '进球',
    'yellow_card' => '黄牌',
    'red_card' => '红牌',
    'penalty' => '点球',
    'match_start' => '比赛开始',
    'match_end' => '比赛结束',
    _ => '比赛有新进展',
  };
  return player?.trim().isNotEmpty == true
      ? '$timing · ${player!.trim()}$label'
      : '$timing · $label';
}
