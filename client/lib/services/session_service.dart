import 'dart:convert';

import 'package:http/http.dart' as http;
import 'package:shared_preferences/shared_preferences.dart';

class SessionCredentials {
  final String userId;
  final String sessionId;
  final String accessToken;
  final String refreshToken;
  final DateTime expiresAt;

  const SessionCredentials({
    required this.userId,
    required this.sessionId,
    required this.accessToken,
    required this.refreshToken,
    required this.expiresAt,
  });

  factory SessionCredentials.fromJson(Map<String, dynamic> json) {
    return SessionCredentials(
      userId: json['userId']?.toString() ?? '',
      sessionId: json['sessionId']?.toString() ?? '',
      accessToken: json['accessToken']?.toString() ?? '',
      refreshToken: json['refreshToken']?.toString() ?? '',
      expiresAt:
          DateTime.tryParse(json['expiresAt']?.toString() ?? '')?.toUtc() ??
              DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
    );
  }

  bool get isUsable =>
      userId.isNotEmpty &&
      sessionId.isNotEmpty &&
      accessToken.isNotEmpty &&
      refreshToken.isNotEmpty &&
      expiresAt
          .isAfter(DateTime.now().toUtc().add(const Duration(seconds: 30)));
}

class SessionService {
  static const _accessTokenKey = 'session_access_token';
  static const _refreshTokenKey = 'session_refresh_token';
  static const _userIdKey = 'session_user_id';
  static const _sessionIdKey = 'session_id';
  static const _expiresAtKey = 'session_expires_at';

  final http.Client _client;

  SessionService({http.Client? client}) : _client = client ?? http.Client();

  Future<SessionCredentials> ensureSession({
    required String baseUrl,
    required String deviceId,
  }) async {
    final stored = await _load();
    if (stored != null && stored.refreshToken.isNotEmpty) {
      try {
        return await _refresh(baseUrl, stored.refreshToken);
      } catch (_) {
        if (stored.isUsable) return stored;
      }
    }
    return _anonymous(baseUrl, deviceId);
  }

  Future<SessionCredentials> _anonymous(String baseUrl, String deviceId) {
    return _request(
      baseUrl,
      '/api/sessions/anonymous',
      {'deviceId': deviceId},
    );
  }

  Future<SessionCredentials> _refresh(
    String baseUrl,
    String refreshToken,
  ) {
    return _request(
      baseUrl,
      '/api/sessions/refresh',
      {'refreshToken': refreshToken},
    );
  }

  Future<SessionCredentials> _request(
    String baseUrl,
    String path,
    Map<String, dynamic> body,
  ) async {
    final response = await _client
        .post(
          Uri.parse('${normalizeAPIBaseURL(baseUrl)}$path'),
          headers: const {'Content-Type': 'application/json'},
          body: jsonEncode(body),
        )
        .timeout(const Duration(seconds: 10));
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw SessionException('session request failed: ${response.statusCode}');
    }
    final decoded = jsonDecode(response.body);
    if (decoded is! Map<String, dynamic>) {
      throw const SessionException('invalid session response');
    }
    final credentials = SessionCredentials.fromJson(decoded);
    if (!credentials.isUsable) {
      throw const SessionException('incomplete session response');
    }
    await _save(credentials);
    return credentials;
  }

  Future<SessionCredentials?> _load() async {
    final prefs = await SharedPreferences.getInstance();
    final expiresAt = DateTime.tryParse(prefs.getString(_expiresAtKey) ?? '');
    final credentials = SessionCredentials(
      userId: prefs.getString(_userIdKey) ?? '',
      sessionId: prefs.getString(_sessionIdKey) ?? '',
      accessToken: prefs.getString(_accessTokenKey) ?? '',
      refreshToken: prefs.getString(_refreshTokenKey) ?? '',
      expiresAt: expiresAt?.toUtc() ??
          DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
    );
    if (credentials.userId.isEmpty || credentials.sessionId.isEmpty) {
      return null;
    }
    return credentials;
  }

  Future<void> _save(SessionCredentials credentials) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_userIdKey, credentials.userId);
    await prefs.setString(_sessionIdKey, credentials.sessionId);
    await prefs.setString(_accessTokenKey, credentials.accessToken);
    await prefs.setString(_refreshTokenKey, credentials.refreshToken);
    await prefs.setString(
        _expiresAtKey, credentials.expiresAt.toIso8601String());
  }

  void close() {
    _client.close();
  }
}

String normalizeAPIBaseURL(String value) {
  final uri = Uri.parse(value.trim());
  final scheme = switch (uri.scheme) {
    'ws' => 'http',
    'wss' => 'https',
    _ => uri.scheme,
  };
  return uri
      .replace(scheme: scheme, path: '', query: null, fragment: null)
      .toString()
      .replaceAll(RegExp(r'/+$'), '');
}

class SessionException implements Exception {
  final String message;

  const SessionException(this.message);

  @override
  String toString() => message;
}
