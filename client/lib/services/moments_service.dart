import 'dart:convert';

import 'package:http/http.dart' as http;

import 'session_service.dart';

/// 共同瞬间(memory-surfacing 1.4/1.5):GET/DELETE /api/me/moments 的客户端面。
/// 数据源 = Moment 投影(embedding_moments,独立持久化);忘掉 = 物理删除
/// (隐私生命周期:用户要求忘掉的内容不留存)。token 经 SessionService
/// ensureSession 获取(与 PortraitService 同形态)。
class SharedMomentsService {
  SharedMomentsService({
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

  Future<List<SharedMomentEntry>> fetch() async {
    final token = await _token();
    final response = await _client.get(
      Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/me/moments'),
      headers: {'Authorization': 'Bearer $token'},
    ).timeout(const Duration(seconds: 5));
    if (response.statusCode != 200) {
      throw const SharedMomentsException('球球这边暂时没连上，稍后再试试');
    }
    final body =
        jsonDecode(utf8.decode(response.bodyBytes)) as Map<String, dynamic>;
    final moments = body['moments'];
    if (moments is! List) return const [];
    return moments
        .whereType<Map<String, dynamic>>()
        .map(SharedMomentEntry.fromJson)
        .where((entry) => entry.content.trim().isNotEmpty)
        .toList(growable: false);
  }

  Future<void> forget({required String momentId}) async {
    final token = await _token();
    final response = await _client.delete(
      Uri.parse(
          '${normalizeAPIBaseURL(baseUrl)}/api/me/moments/${Uri.encodeComponent(momentId)}'),
      headers: {'Authorization': 'Bearer $token'},
    ).timeout(const Duration(seconds: 5));
    if (response.statusCode != 200) {
      throw const SharedMomentsException('忘掉失败，稍后再试试');
    }
  }

  Future<String> _token() async {
    final session = await _sessions.ensureSession(
      baseUrl: normalizeAPIBaseURL(baseUrl),
      deviceId: deviceId,
    );
    return session.accessToken;
  }
}

class SharedMomentEntry {
  final String id;
  final String kind;
  final String content;
  final double importance;
  final String? occurredAt;

  const SharedMomentEntry({
    required this.id,
    required this.kind,
    required this.content,
    required this.importance,
    this.occurredAt,
  });

  factory SharedMomentEntry.fromJson(Map<String, dynamic> json) =>
      SharedMomentEntry(
        id: json['id']?.toString() ?? '',
        kind: json['kind']?.toString() ?? '',
        content: json['content']?.toString() ?? '',
        importance: (json['importance'] as num?)?.toDouble() ?? 0,
        occurredAt: json['occurredAt']?.toString(),
      );

  /// 瞬间的中文引导语(kind → 前缀),内容原样。
  String get displayLabel {
    switch (kind) {
      case 'match_event':
        return '比赛瞬间';
      case 'promise':
        return '约定';
      case 'emotional_exchange':
        return '情绪时刻';
      case 'user_fact':
        return '你说过';
      default:
        return '共同瞬间';
    }
  }
}

class SharedMomentsException implements Exception {
  final String message;
  const SharedMomentsException(this.message);
  @override
  String toString() => message;
}
