import 'dart:convert';

import 'package:http/http.dart' as http;

import 'session_service.dart';

/// 球友手记（teammate-journal）：/api/me/journal 的客户端面——列表/点赞/
/// 忘掉（物理删除）+ 赛季记忆册（album：每场一条=比分+手记摘要+共同瞬间）。
/// token 经 SessionService ensureSession 获取（与 SharedMomentsService 同
/// 形态）。手记是阅读面：零编造比分由后端确定性校验保证，本层不重复判。
class JournalService {
  JournalService({
    required this.baseUrl,
    required SessionService sessions,
    required this.deviceId,
    http.Client? client,
  })  : _sessions = sessions,
        _client = client ?? http.Client();

  final String baseUrl;
  final SessionService _sessions;
  final String deviceId;
  final http.Client _client;

  Future<Map<String, String>> _headers() async {
    final session = await _sessions.ensureSession(
      baseUrl: normalizeAPIBaseURL(baseUrl),
      deviceId: deviceId,
    );
    return {
      'Authorization': 'Bearer ${session.accessToken}',
      'Content-Type': 'application/json',
    };
  }

  /// 手记列表（创建时间倒序）。
  Future<List<JournalEntry>> fetch() async {
    final response = await _client.get(
      Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/me/journal'),
      headers: await _headers(),
    );
    if (response.statusCode != 200) {
      throw JournalException('手记加载失败 (${response.statusCode})');
    }
    final decoded = jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
    final entries = (decoded['entries'] as List<dynamic>? ?? [])
        .map((item) => JournalEntry.fromJson(item as Map<String, dynamic>))
        .toList();
    return entries;
  }

  /// 点赞/取消。
  Future<void> setLiked(String entryId, bool liked) async {
    final response = await _client.put(
      Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/me/journal/$entryId/like'),
      headers: await _headers(),
      body: jsonEncode({'liked': liked}),
    );
    if (response.statusCode != 200) {
      throw JournalException('点赞失败 (${response.statusCode})');
    }
  }

  /// 忘掉一篇手记（物理删除——隐私生命周期：要求忘掉的内容不留存）。
  Future<void> forget(String entryId) async {
    final response = await _client.delete(
      Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/me/journal/$entryId'),
      headers: await _headers(),
    );
    if (response.statusCode != 200) {
      throw JournalException('忘掉手记失败 (${response.statusCode})');
    }
  }

  /// 赛季记忆册（[season] 空 = 全部赛季）。
  Future<List<AlbumPage>> fetchAlbum({String season = ''}) async {
    final query = season.isEmpty ? '' : '?season=${Uri.encodeComponent(season)}';
    final response = await _client.get(
      Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/me/journal/album$query'),
      headers: await _headers(),
    );
    if (response.statusCode != 200) {
      throw JournalException('赛季册加载失败 (${response.statusCode})');
    }
    final decoded = jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
    final album = (decoded['album'] as List<dynamic>? ?? [])
        .map((item) => AlbumPage.fromJson(item as Map<String, dynamic>))
        .toList();
    return album;
  }
}

class JournalException implements Exception {
  JournalException(this.message);

  final String message;

  @override
  String toString() => message;
}

/// 一篇手记：比赛快照（队名/比分/进球，账本终场投影的引用）+ 第一人称正文。
class JournalEntry {
  JournalEntry({
    required this.id,
    required this.matchId,
    required this.homeTeam,
    required this.awayTeam,
    required this.score,
    required this.body,
    required this.liked,
    this.goals = const [],
    this.season,
    this.createdAt,
  });

  factory JournalEntry.fromJson(Map<String, dynamic> json) => JournalEntry(
        id: json['id'] as String,
        matchId: json['matchId'] as String,
        homeTeam: (json['homeTeam'] ?? '') as String,
        awayTeam: (json['awayTeam'] ?? '') as String,
        score: (json['score'] ?? '') as String,
        body: (json['body'] ?? '') as String,
        liked: (json['liked'] ?? false) as bool,
        goals: (json['goals'] as List<dynamic>? ?? []).cast<String>(),
        season: json['season'] as String?,
        createdAt: json['createdAt'] as String?,
      );

  final String id;
  final String matchId;
  final String homeTeam;
  final String awayTeam;
  final String score;
  final List<String> goals;
  final String body;
  final String? season;
  final String? createdAt;
  bool liked;
}

/// 赛季册一页：一场一条——比赛快照+手记摘要+共同瞬间。
class AlbumPage {
  AlbumPage({
    required this.matchId,
    required this.homeTeam,
    required this.awayTeam,
    required this.score,
    required this.liked,
    this.season,
    this.entryId,
    this.journal,
    this.moments = const [],
    this.createdAt,
  });

  factory AlbumPage.fromJson(Map<String, dynamic> json) => AlbumPage(
        matchId: json['matchId'] as String,
        homeTeam: (json['homeTeam'] ?? '') as String,
        awayTeam: (json['awayTeam'] ?? '') as String,
        score: (json['score'] ?? '') as String,
        liked: (json['liked'] ?? false) as bool,
        season: json['season'] as String?,
        entryId: json['entryId'] as String?,
        journal: json['journal'] as String?,
        moments: (json['moments'] as List<dynamic>? ?? []).cast<String>(),
        createdAt: json['createdAt'] as String?,
      );

  final String matchId;
  final String? season;
  final String homeTeam;
  final String awayTeam;
  final String score;
  final String? entryId;
  final String? journal;
  final List<String> moments;
  final String? createdAt;
  final bool liked;
}
