import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
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
  final SessionSecretStore _secretStorage;
  final Map<String, String> _memorySecrets = <String, String>{};

  SessionService({http.Client? client, SessionSecretStore? secretStorage})
      : _client = client ?? http.Client(),
        _secretStorage = secretStorage ?? FlutterSessionSecretStore();

  Future<SessionCredentials> ensureSession({
    required String baseUrl,
    required String deviceId,
  }) async {
    final stored = await _load();
    if (stored != null && stored.refreshToken.isNotEmpty) {
      try {
        return await _refresh(baseUrl, stored.refreshToken);
      } catch (error) {
        if (error is SessionException && error.statusCode == 401) {
          await _clear();
          return _anonymous(baseUrl, deviceId);
        }
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
      throw SessionException(
        'session request failed: ${response.statusCode}',
        statusCode: response.statusCode,
      );
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
    final storedAccess = await _readSecret(_accessTokenKey, prefs);
    final storedRefresh = await _readSecret(_refreshTokenKey, prefs);
    final credentials = SessionCredentials(
      userId: prefs.getString(_userIdKey) ?? '',
      sessionId: prefs.getString(_sessionIdKey) ?? '',
      accessToken: storedAccess,
      refreshToken: storedRefresh,
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
    await _writeSecret(_accessTokenKey, credentials.accessToken);
    await _writeSecret(_refreshTokenKey, credentials.refreshToken);
    await prefs.remove(_accessTokenKey);
    await prefs.remove(_refreshTokenKey);
    await prefs.setString(
        _expiresAtKey, credentials.expiresAt.toIso8601String());
  }

  Future<String> _readSecret(String key, SharedPreferences prefs) async {
    final memoryValue = _memorySecrets[key];
    if (memoryValue != null && memoryValue.isNotEmpty) return memoryValue;
    String? secureValue;
    try {
      secureValue = await _secretStorage.read(key);
    } catch (_) {
      secureValue = null;
    }
    if (secureValue != null && secureValue.isNotEmpty) return secureValue;
    final legacyValue = prefs.getString(key) ?? '';
    if (legacyValue.isNotEmpty) {
      await _writeSecret(key, legacyValue);
      if (_memorySecrets[key] == null) await prefs.remove(key);
    }
    return legacyValue;
  }

  Future<void> _writeSecret(String key, String value) async {
    try {
      await _secretStorage.write(key, value);
      _memorySecrets.remove(key);
    } catch (_) {
      _memorySecrets[key] = value;
    }
  }

  Future<void> _deleteSecret(String key) async {
    _memorySecrets.remove(key);
    try {
      await _secretStorage.delete(key);
    } catch (_) {}
  }

  Future<void> _clear() async {
    final prefs = await SharedPreferences.getInstance();
    await _deleteSecret(_accessTokenKey);
    await _deleteSecret(_refreshTokenKey);
    await prefs.remove(_userIdKey);
    await prefs.remove(_sessionIdKey);
    await prefs.remove(_expiresAtKey);
    await prefs.remove(_accessTokenKey);
    await prefs.remove(_refreshTokenKey);
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
  return Uri(
    scheme: scheme,
    userInfo: uri.userInfo,
    host: uri.host,
    port: uri.hasPort ? uri.port : null,
  ).toString();
}

class SessionException implements Exception {
  final String message;
  final int? statusCode;

  const SessionException(this.message, {this.statusCode});

  @override
  String toString() => message;
}

abstract interface class SessionSecretStore {
  Future<String?> read(String key);

  Future<void> write(String key, String value);

  Future<void> delete(String key);
}

class FlutterSessionSecretStore implements SessionSecretStore {
  static const _storage = FlutterSecureStorage();

  @override
  Future<String?> read(String key) => _storage.read(key: key);

  @override
  Future<void> write(String key, String value) =>
      _storage.write(key: key, value: value);

  @override
  Future<void> delete(String key) => _storage.delete(key: key);
}
