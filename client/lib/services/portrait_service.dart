import 'dart:async';
import 'dart:convert';

import 'package:http/http.dart' as http;

import 'session_service.dart';

/// One entry of the synthesized user portrait (球球懂我).
class PortraitEntryData {
  final String id;
  final String topic;
  final String topicLabel;
  final String subTopic;
  final String label;
  final String content;
  final DateTime? updatedAt;
  final String source;

  const PortraitEntryData({
    required this.id,
    required this.topic,
    required this.topicLabel,
    required this.subTopic,
    required this.label,
    required this.content,
    required this.updatedAt,
    required this.source,
  });

  factory PortraitEntryData.fromJson(Map<String, dynamic> json) {
    return PortraitEntryData(
      id: json['id']?.toString() ?? '',
      topic: json['topic']?.toString() ?? '',
      topicLabel: json['topicLabel']?.toString() ?? '',
      subTopic: json['subTopic']?.toString() ?? '',
      label: json['label']?.toString() ?? '',
      content: json['content']?.toString() ?? '',
      updatedAt:
          DateTime.tryParse(json['updatedAt']?.toString() ?? '')?.toUtc(),
      source: json['source']?.toString() ?? '',
    );
  }

  /// True when the content comes from an explicit user edit rather than
  /// Memobase synthesis.
  bool get isUserEdited => source == 'user';
}

/// The user's merged portrait: Memobase synthesis layered with the user's own
/// edits and deletion tombstones.
class PortraitData {
  final List<PortraitEntryData> entries;
  final DateTime? updatedAt;

  const PortraitData({required this.entries, required this.updatedAt});

  factory PortraitData.fromJson(Map<String, dynamic> json) {
    final raw = json['entries'];
    final entries = <PortraitEntryData>[];
    if (raw is List) {
      for (final item in raw) {
        if (item is Map<String, dynamic>) {
          entries.add(PortraitEntryData.fromJson(item));
        }
      }
    }
    return PortraitData(
      entries: entries,
      updatedAt:
          DateTime.tryParse(json['updatedAt']?.toString() ?? '')?.toUtc(),
    );
  }

  bool get isEmpty => entries.isEmpty;
}

class PortraitException implements Exception {
  final String message;
  final int? statusCode;

  const PortraitException(this.message, {this.statusCode});

  @override
  String toString() => message;
}

/// Reads and writes the real stored portrait: GET/PATCH/DELETE /api/me/portrait
/// with the session bearer token, the same transport as the privacy API.
class PortraitService {
  PortraitService({
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

  Future<PortraitData> load() {
    return _send((token) => _client.get(_portraitUri(), headers: _headers(token)));
  }

  Future<PortraitData> edit({
    required String topic,
    required String subTopic,
    required String content,
    String? entryId,
  }) {
    return _send(
      (token) => _client.patch(
        _portraitUri(),
        headers: _headers(token),
        body: jsonEncode({
          'topic': topic,
          'subTopic': subTopic,
          'content': content,
          if (entryId != null && entryId.isNotEmpty) 'entryId': entryId,
        }),
      ),
    );
  }

  Future<PortraitData> forgetOne({
    required String topic,
    required String subTopic,
    String? entryId,
  }) {
    return _send(
      (token) => _client.delete(
        _portraitUri(),
        headers: _headers(token),
        body: jsonEncode({
          'topic': topic,
          'subTopic': subTopic,
          if (entryId != null && entryId.isNotEmpty) 'entryId': entryId,
        }),
      ),
    );
  }

  Future<PortraitData> forgetAll() {
    return _send(
      (token) => _client.delete(
        _portraitUri(),
        headers: _headers(token),
        body: '{}',
      ),
    );
  }

  Uri _portraitUri() =>
      Uri.parse('${normalizeAPIBaseURL(baseUrl)}/api/me/portrait');

  Map<String, String> _headers(String token) => {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer $token',
      };

  Future<PortraitData> _send(
    Future<http.Response> Function(String token) request,
  ) async {
    final session = await _sessions.ensureSession(
      baseUrl: normalizeAPIBaseURL(baseUrl),
      deviceId: deviceId,
    );
    final http.Response response;
    try {
      response = await request(session.accessToken).timeout(
        const Duration(seconds: 10),
      );
    } on TimeoutException {
      throw const PortraitException('球球这边暂时没连上，稍后再试试');
    }
    if (response.statusCode >= 200 && response.statusCode < 300) {
      final decoded = jsonDecode(response.body);
      if (decoded is! Map<String, dynamic>) {
        throw const PortraitException('球球返回的画像格式不对');
      }
      return PortraitData.fromJson(decoded);
    }
    throw PortraitException(
      _messageFor(response.statusCode),
      statusCode: response.statusCode,
    );
  }

  static String _messageFor(int statusCode) {
    switch (statusCode) {
      case 401:
        return '会话过期了，请重试';
      case 403:
        return '当前身份没有权限查看画像';
      case 409:
        return '账号数据删除正在进行中';
      case 410:
        return '这份画像已经随账号一起删除了';
      case 503:
        return '球球的记忆暂时不可用，稍后再试试';
      default:
        return '操作没有成功，稍后再试试';
    }
  }

  void close() {
    _client.close();
  }
}
