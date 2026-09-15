import 'dart:convert';

import 'package:http/http.dart' as http;

import 'session_service.dart';

class MatchPlayerOverview {
  final String name;
  final String number;
  final String position;
  final String lineup;

  const MatchPlayerOverview({
    required this.name,
    this.number = '',
    this.position = '',
    this.lineup = '',
  });

  factory MatchPlayerOverview.fromJson(Map<String, dynamic> json) {
    return MatchPlayerOverview(
      name: json['name']?.toString() ?? '',
      number: json['number']?.toString() ?? '',
      position: json['position']?.toString() ?? '',
      lineup: json['lineup']?.toString() ?? '',
    );
  }

  bool get isBench => lineup == 'bench';
}

class MatchEventOverview {
  final String id;
  final String clock;
  final String period;
  final String eventType;
  final String teamId;
  final String teamName;
  final String playerName;
  final String description;
  final int homeScore;
  final int awayScore;

  const MatchEventOverview({
    this.id = '',
    this.clock = '',
    this.period = '',
    this.eventType = '',
    this.teamId = '',
    this.teamName = '',
    this.playerName = '',
    this.description = '',
    this.homeScore = 0,
    this.awayScore = 0,
  });

  factory MatchEventOverview.fromJson(Map<String, dynamic> json) {
    final score = _map(json['score']);
    return MatchEventOverview(
      id: json['id']?.toString() ?? '',
      clock: json['clock']?.toString() ?? '',
      period: json['period']?.toString() ?? '',
      eventType: json['eventType']?.toString() ?? '',
      teamId: json['teamId']?.toString() ?? '',
      teamName: json['teamName']?.toString() ?? '',
      playerName: json['playerName']?.toString() ?? '',
      description: json['description']?.toString() ?? '',
      homeScore: _integer(score?['home']),
      awayScore: _integer(score?['away']),
    );
  }

  String get label {
    if (description.trim().isNotEmpty) return description.trim();
    final actor = playerName.trim().isNotEmpty ? ' · $playerName' : '';
    return switch (eventType) {
      'goal' => '进球$actor',
      'yellow_card' => '黄牌$actor',
      'red_card' => '红牌$actor',
      'penalty' || 'penalty_awarded' => '点球$actor',
      'substitution' => '换人',
      'var_check' => 'VAR 检查',
      'var_result' => 'VAR 结果',
      'injury' => '伤停$actor',
      'halftime' => '中场休息',
      'fulltime' || 'match_end' => '比赛结束',
      _ => '比赛动态$actor',
    };
  }

  String get displayClock => clock.trim().isEmpty ? '—' : clock.trim();
}

class MatchTeamStat {
  final String key;
  final String label;
  final double home;
  final double away;
  final String unit;

  const MatchTeamStat({
    required this.key,
    required this.label,
    required this.home,
    required this.away,
    this.unit = '',
  });

  factory MatchTeamStat.fromJson(Map<String, dynamic> json) {
    return MatchTeamStat(
      key: json['key']?.toString() ?? '',
      label: json['label']?.toString() ?? '',
      home: _decimal(json['home']),
      away: _decimal(json['away']),
      unit: json['unit']?.toString() ?? '',
    );
  }
}

class MatchOverviewData {
  final String matchId;
  final String homeTeam;
  final String awayTeam;
  final String competition;
  final String kickoff;
  final String round;
  final String venue;
  final String referee;
  final String homeCoach;
  final String awayCoach;
  final String homeFormation;
  final String awayFormation;
  final int homeScore;
  final int awayScore;
  final int? halftimeHomeScore;
  final int? halftimeAwayScore;
  final String period;
  final String clock;
  final String liveLabel;
  final List<MatchPlayerOverview> homePlayers;
  final List<MatchPlayerOverview> awayPlayers;
  final List<MatchEventOverview> events;
  final List<MatchTeamStat> teamStats;
  final Map<String, num> stats;

  const MatchOverviewData({
    this.matchId = '',
    this.homeTeam = '主队',
    this.awayTeam = '客队',
    this.competition = '',
    this.kickoff = '',
    this.round = '',
    this.venue = '',
    this.referee = '',
    this.homeCoach = '',
    this.awayCoach = '',
    this.homeFormation = '',
    this.awayFormation = '',
    this.homeScore = 0,
    this.awayScore = 0,
    this.halftimeHomeScore,
    this.halftimeAwayScore,
    this.period = 'pre_match',
    this.clock = '',
    this.liveLabel = '等待开赛',
    this.homePlayers = const [],
    this.awayPlayers = const [],
    this.events = const [],
    this.teamStats = const [],
    this.stats = const {},
  });

  factory MatchOverviewData.fromPayload({
    required Map<String, dynamic> config,
    required Map<String, dynamic> snapshot,
    required List<Map<String, dynamic>> events,
  }) {
    final score = _map(snapshot['score']);
    final parsedEvents = <MatchEventOverview>[
      ...events.map(MatchEventOverview.fromJson),
    ];
    if (parsedEvents.isEmpty) {
      final recent = snapshot['recentEvents'];
      if (recent is List) {
        parsedEvents.addAll(
          recent.whereType<Map<String, dynamic>>().map(
                MatchEventOverview.fromJson,
              ),
        );
      }
    }
    final halftimeEvent =
        parsedEvents.where((event) => event.eventType == 'halftime').lastOrNull;
    final period = snapshot['period']?.toString() ?? 'pre_match';
    return MatchOverviewData(
      matchId: config['matchId']?.toString() ??
          snapshot['matchId']?.toString() ??
          '',
      homeTeam: config['homeTeam']?.toString() ??
          snapshot['homeTeam']?.toString() ??
          '主队',
      awayTeam: config['awayTeam']?.toString() ??
          snapshot['awayTeam']?.toString() ??
          '客队',
      competition: config['competition']?.toString() ??
          snapshot['competition']?.toString() ??
          '',
      kickoff: config['kickoff']?.toString() ?? '',
      round: config['round']?.toString() ?? '',
      venue: config['venue']?.toString() ?? '',
      referee: config['referee']?.toString() ?? '',
      homeCoach: config['homeCoach']?.toString() ?? '',
      awayCoach: config['awayCoach']?.toString() ?? '',
      homeFormation: config['homeFormation']?.toString() ?? '',
      awayFormation: config['awayFormation']?.toString() ?? '',
      homeScore: _integer(score?['home']),
      awayScore: _integer(score?['away']),
      halftimeHomeScore: halftimeEvent?.homeScore,
      halftimeAwayScore: halftimeEvent?.awayScore,
      period: period,
      clock: snapshot['clock']?.toString() ?? '',
      liveLabel: _liveLabel(period),
      homePlayers: _players(config['homePlayers']),
      awayPlayers: _players(config['awayPlayers']),
      events: parsedEvents,
      teamStats: _teamStats(config['stats']),
      stats: _stats(snapshot['stats']),
    );
  }

  MatchOverviewData withSnapshot(Map<String, dynamic> snapshot) {
    final score = _map(snapshot['score']);
    final incoming = snapshot['recentEvents'];
    final merged = <String, MatchEventOverview>{
      for (final event in events) _eventKey(event): event,
    };
    if (incoming is List) {
      for (final raw in incoming.whereType<Map<String, dynamic>>()) {
        final event = MatchEventOverview.fromJson(raw);
        merged[_eventKey(event)] = event;
      }
    }
    final nextPeriod = snapshot['period']?.toString() ?? period;
    return copyWith(
      homeScore: _integer(score?['home']),
      awayScore: _integer(score?['away']),
      period: nextPeriod,
      clock: snapshot['clock']?.toString() ?? clock,
      liveLabel: _liveLabel(nextPeriod),
      events: merged.values.toList(growable: false),
    );
  }

  MatchOverviewData copyWith({
    int? homeScore,
    int? awayScore,
    String? period,
    String? clock,
    String? liveLabel,
    List<MatchEventOverview>? events,
  }) {
    return MatchOverviewData(
      matchId: matchId,
      homeTeam: homeTeam,
      awayTeam: awayTeam,
      competition: competition,
      kickoff: kickoff,
      round: round,
      venue: venue,
      referee: referee,
      homeCoach: homeCoach,
      awayCoach: awayCoach,
      homeFormation: homeFormation,
      awayFormation: awayFormation,
      homeScore: homeScore ?? this.homeScore,
      awayScore: awayScore ?? this.awayScore,
      halftimeHomeScore: halftimeHomeScore,
      halftimeAwayScore: halftimeAwayScore,
      period: period ?? this.period,
      clock: clock ?? this.clock,
      liveLabel: liveLabel ?? this.liveLabel,
      homePlayers: homePlayers,
      awayPlayers: awayPlayers,
      events: events ?? this.events,
      teamStats: teamStats,
      stats: stats,
    );
  }

  List<MatchPlayerOverview> get homeStarters =>
      homePlayers.where((player) => !player.isBench).toList(growable: false);
  List<MatchPlayerOverview> get awayStarters =>
      awayPlayers.where((player) => !player.isBench).toList(growable: false);
  List<MatchPlayerOverview> get homeBench =>
      homePlayers.where((player) => player.isBench).toList(growable: false);
  List<MatchPlayerOverview> get awayBench =>
      awayPlayers.where((player) => player.isBench).toList(growable: false);

  List<MatchEventOverview> get chronologicalEvents =>
      events.reversed.toList(growable: false);

  List<MatchEventOverview> get disciplinaryEvents => events
      .where((event) =>
          event.eventType == 'yellow_card' || event.eventType == 'red_card')
      .toList(growable: false);

  int cardCount(String teamId, String eventType) => disciplinaryEvents
      .where((event) => event.teamId == teamId && event.eventType == eventType)
      .length;
}

class MatchOverviewService {
  final http.Client _client;

  MatchOverviewService({http.Client? client})
      : _client = client ?? http.Client();

  Future<MatchOverviewData> fetch({
    required String baseUrl,
    required String matchId,
  }) async {
    final root = normalizeAPIBaseURL(baseUrl);
    final encodedId = Uri.encodeComponent(matchId);
    final configResponse = await _client
        .get(Uri.parse('$root/api/matches/$encodedId/config'))
        .timeout(const Duration(seconds: 8));
    if (configResponse.statusCode < 200 || configResponse.statusCode >= 300) {
      throw MatchOverviewException('比赛资料加载失败：${configResponse.statusCode}');
    }
    final configPayload = _decodeObject(configResponse.body);
    final config = _map(configPayload['config']) ?? const <String, dynamic>{};
    final snapshot =
        _map(configPayload['snapshot']) ?? const <String, dynamic>{};

    var events = <Map<String, dynamic>>[];
    try {
      final eventsResponse = await _client
          .get(Uri.parse('$root/api/matches/$encodedId/events'))
          .timeout(const Duration(seconds: 8));
      if (eventsResponse.statusCode >= 200 && eventsResponse.statusCode < 300) {
        final payload = _decodeObject(eventsResponse.body);
        final rawEvents = payload['events'];
        if (rawEvents is List) {
          events = rawEvents.whereType<Map<String, dynamic>>().toList();
        }
      }
    } catch (_) {
      // The snapshot still gives the user a useful overview when history is unavailable.
    }
    return MatchOverviewData.fromPayload(
      config: config,
      snapshot: snapshot,
      events: events,
    );
  }

  void close() => _client.close();
}

class MatchOverviewException implements Exception {
  final String message;

  const MatchOverviewException(this.message);

  @override
  String toString() => message;
}

List<MatchPlayerOverview> _players(dynamic value) {
  if (value is! List) return const [];
  return value
      .whereType<Map<String, dynamic>>()
      .map(MatchPlayerOverview.fromJson)
      .where((player) => player.name.trim().isNotEmpty)
      .toList(growable: false);
}

Map<String, num> _stats(dynamic value) {
  if (value is! Map) return const {};
  return value.map((key, value) => MapEntry(
        key.toString(),
        value is num ? value : num.tryParse(value.toString()) ?? 0,
      ));
}

List<MatchTeamStat> _teamStats(dynamic value) {
  if (value is! List) return const [];
  return value
      .whereType<Map<String, dynamic>>()
      .map(MatchTeamStat.fromJson)
      .where(
          (stat) => stat.key.trim().isNotEmpty && stat.label.trim().isNotEmpty)
      .toList(growable: false);
}

Map<String, dynamic> _decodeObject(String body) {
  final decoded = jsonDecode(body);
  if (decoded is! Map) throw const MatchOverviewException('比赛资料格式无效');
  return decoded.cast<String, dynamic>();
}

Map<String, dynamic>? _map(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return value.cast<String, dynamic>();
  return null;
}

int _integer(dynamic value) =>
    value is num ? value.toInt() : int.tryParse(value?.toString() ?? '') ?? 0;

double _decimal(dynamic value) => value is num
    ? value.toDouble()
    : double.tryParse(value?.toString() ?? '') ?? 0;

String _eventKey(MatchEventOverview event) => event.id.isNotEmpty
    ? event.id
    : '${event.clock}|${event.eventType}|${event.playerName}|${event.description}';

String _liveLabel(String period) => switch (period.trim().toLowerCase()) {
      '' || 'pre_match' => '等待开赛',
      'finished' || 'full_time' || 'fulltime' => '已结束',
      _ => '直播中',
    };
