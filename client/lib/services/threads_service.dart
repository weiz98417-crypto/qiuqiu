import 'dart:convert';

import 'package:http/http.dart' as http;

import 'session_service.dart';

/// 未完话题条(memory-surfacing 1.7):GET /api/me/threads 的客户端面。
/// 只读——续聊 = 把话题文本作为下一句 user_speech 发出(走既有对话路径),
/// 关闭由服务端 recovery 投递语义处理,客户端不设删除口。
class OpenThreadsService {
  OpenThreadsService({http.Client? client}) : _client = client ?? http.Client();

  final http.Client _client;

  Future<List<OpenThreadEntry>> fetch({
    required String baseUrl,
    required String accessToken,
  }) async {
    final response = await _client
        .get(
          Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/me/threads'),
          headers: {'Authorization': 'Bearer $accessToken'},
        )
        .timeout(const Duration(seconds: 5));
    if (response.statusCode != 200) {
      throw OpenThreadsException('加载未完话题失败 (${response.statusCode})');
    }
    final body = jsonDecode(utf8.decode(response.bodyBytes))
        as Map<String, dynamic>;
    final threads = body['threads'];
    if (threads is! List) return const [];
    return threads
        .whereType<Map<String, dynamic>>()
        .map(OpenThreadEntry.fromJson)
        .where((entry) => entry.content.trim().isNotEmpty)
        .toList(growable: false);
  }
}

class OpenThreadEntry {
  final String id;
  final String kind;
  final String content;
  final String? createdAt;

  const OpenThreadEntry({
    required this.id,
    required this.kind,
    required this.content,
    this.createdAt,
  });

  factory OpenThreadEntry.fromJson(Map<String, dynamic> json) =>
      OpenThreadEntry(
        id: json['id']?.toString() ?? '',
        kind: json['kind']?.toString() ?? '',
        content: json['content']?.toString() ?? '',
        createdAt: json['createdAt']?.toString(),
      );

  /// 话题条上展示的一句中文(kind → 引导语),内容原样。
  String get displayLabel {
    switch (kind) {
      case 'promise':
        return '待跟进';
      case 'unanswered_question':
        return '没答完的问题';
      case 'emotional_moment':
        return '当时的情绪';
      case 'prediction':
        return '说过的话';
      default:
        return '没聊完';
    }
  }
}

class OpenThreadsException implements Exception {
  final String message;
  const OpenThreadsException(this.message);
  @override
  String toString() => message;
}
