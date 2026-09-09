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
    this.homeScore = 0,
    this.awayScore = 0,
  });

  factory MatchCatalogItem.fromJson(Map<String, dynamic> json) {
    int integer(String key) => int.tryParse(json[key]?.toString() ?? '') ?? 0;
    return MatchCatalogItem(
      matchId: json['matchId']?.toString() ?? '',
      homeTeam: json['homeTeam']?.toString() ?? '主队',
      awayTeam: json['awayTeam']?.toString() ?? '客队',
      competition: json['competition']?.toString() ?? '',
      kickoff: json['kickoff']?.toString() ?? '',
      liveLabel: json['liveLabel']?.toString() ?? '等待开赛',
      status: json['status']?.toString() ?? 'scheduled',
      homeScore: integer('homeScore'),
      awayScore: integer('awayScore'),
    );
  }

  String get title => '$homeTeam vs $awayTeam';
}

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
