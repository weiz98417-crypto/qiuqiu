import 'dart:convert';

import 'package:http/http.dart' as http;

import 'session_service.dart';

class MatchCatalogItem {
  final String matchId;
  final String homeTeam;
  final String awayTeam;
  final String competition;
  final String kickoff;
  final String liveLabel;
  final String status;
  final String lifecycle;
  final int homeScore;
  final int awayScore;

  const MatchCatalogItem({
    required this.matchId,
    required this.homeTeam,
    required this.awayTeam,
    this.competition = '',
    this.kickoff = '',
    this.liveLabel = '等待开赛',
    this.status = 'scheduled',
    this.lifecycle = '',
    this.homeScore = 0,
    this.awayScore = 0,
  });

  factory MatchCatalogItem.fromJson(Map<String, dynamic> json) {
    int integer(String key) => int.tryParse(json[key]?.toString() ?? '') ?? 0;
    return MatchCatalogItem(
      matchId: json['matchId']?.toString() ?? '',
      homeTeam: _catalogNameZh(json['homeTeam']?.toString() ?? '主队'),
      awayTeam: _catalogNameZh(json['awayTeam']?.toString() ?? '客队'),
      competition: _catalogCompetitionZh(json['competition']?.toString() ?? ''),
      kickoff: json['kickoff']?.toString() ?? '',
      liveLabel: json['liveLabel']?.toString() ?? '等待开赛',
      status: json['status']?.toString() ?? 'scheduled',
      lifecycle: json['lifecycle']?.toString() ?? '',
      homeScore: integer('homeScore'),
      awayScore: integer('awayScore'),
    );
  }

  String get title => '$homeTeam vs $awayTeam';
}

const _catalogTeamNamesZh = <String, String>{
  'Arsenal': '阿森纳',
  'Coventry City': '考文垂城',
  'Liverpool': '利物浦',
  'Nottingham Forest': '诺丁汉森林',
  'Athletic Club': '毕尔巴鄂竞技',
  'Atlético Madrid': '马德里竞技',
  'Levante': '莱万特',
  'Real Betis': '皇家贝蒂斯',
  'Internazionale': '国际米兰',
  'Napoli': '那不勒斯',
  'Juventus': '尤文图斯',
  'Parma': '帕尔马',
  'England': '英格兰',
  'Congo DR': '刚果（金）',
};

String _catalogNameZh(String value) => _catalogTeamNamesZh[value] ?? value;

String _catalogCompetitionZh(String value) => value
    .replaceAll('English Premier League', '英格兰超级联赛')
    .replaceAll('Premier League', '英超')
    .replaceAll('La Liga', '西甲')
    .replaceAll('Serie A', '意甲')
    .replaceAll('FIFA World Cup', '国际足联世界杯');

class MatchCatalogService {
  final http.Client _client;

  MatchCatalogService({http.Client? client})
      : _client = client ?? http.Client();

  Future<List<MatchCatalogItem>> fetch(String baseUrl) async {
    final response = await _client
        .get(Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/matches/catalog'))
        .timeout(const Duration(seconds: 8));
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw MatchCatalogException('比赛列表加载失败：${response.statusCode}');
    }
    final decoded = jsonDecode(response.body);
    if (decoded is! Map<String, dynamic> || decoded['matches'] is! List) {
      throw const MatchCatalogException('比赛列表格式无效');
    }
    return (decoded['matches'] as List)
        .whereType<Map<String, dynamic>>()
        .map(MatchCatalogItem.fromJson)
        .where((item) => item.matchId.isNotEmpty)
        .toList(growable: false);
  }

  void close() => _client.close();
}

class MatchCatalogException implements Exception {
  final String message;

  const MatchCatalogException(this.message);

  @override
  String toString() => message;
}
