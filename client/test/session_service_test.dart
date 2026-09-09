import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qiuqiu/services/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('normalizes websocket URLs to the session API origin', () {
    expect(
      normalizeAPIBaseURL('wss://qiuqiu.example/ws/match/test'),
      'https://qiuqiu.example',
    );
    expect(
      normalizeAPIBaseURL('ws://10.0.2.2:8080/ws/match/test'),
      'http://10.0.2.2:8080',
    );
    expect(
      normalizeAPIBaseURL(
          'ws://127.0.0.1:8080/ws/match/demo-user-facing-flow?matchId=demo-user-facing-flow#live'),
      'http://127.0.0.1:8080',
    );
  });

  test('creates an anonymous session and persists rotated credentials',
      () async {
    SharedPreferences.setMockInitialValues({});
    var requestCount = 0;
    final client = MockClient((request) async {
      requestCount++;
      expect(request.url.path, '/api/sessions/anonymous');
      expect(jsonDecode(request.body), {'deviceId': 'device_123'});
      return http.Response(
        jsonEncode({
          'userId': 'usr_server',
          'sessionId': 'ses_server',
          'accessToken': 'access_token',
          'refreshToken': 'refresh_token',
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(minutes: 15))
              .toIso8601String(),
        }),
        201,
        headers: {'content-type': 'application/json'},
      );
    });
    final secretStorage = MemorySessionSecretStore();
    final service = SessionService(
      client: client,
      secretStorage: secretStorage,
    );

    final credentials = await service.ensureSession(
      baseUrl: 'ws://127.0.0.1:8080/ws/match/test',
      deviceId: 'device_123',
    );

    expect(requestCount, 1);
    expect(credentials.userId, 'usr_server');
    final prefs = await SharedPreferences.getInstance();
    expect(secretStorage.values['session_access_token'], 'access_token');
    expect(prefs.getString('session_access_token'), isNull);
  });

  test('uses the server user id instead of a client supplied identity', () {
    final credentials = SessionCredentials.fromJson({
      'userId': 'usr_server_bound',
      'sessionId': 'ses_server_bound',
      'accessToken': 'access',
      'refreshToken': 'refresh',
      'expiresAt': DateTime.now()
          .toUtc()
          .add(const Duration(minutes: 15))
          .toIso8601String(),
    });

    expect(credentials.userId, 'usr_server_bound');
  });

  test('keeps the client online when secure storage is unavailable', () async {
    SharedPreferences.setMockInitialValues({});
    final paths = <String>[];
    final client = MockClient((request) async {
      paths.add(request.url.path);
      return http.Response(
        jsonEncode({
          'userId': 'usr_memory',
          'sessionId': 'ses_memory',
          'accessToken': 'access_memory',
          'refreshToken': 'refresh_memory',
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(minutes: 15))
              .toIso8601String(),
        }),
        request.url.path.endsWith('/anonymous') ? 201 : 200,
      );
    });
    final service = SessionService(
      client: client,
      secretStorage: ThrowingSessionSecretStore(),
    );

    await service.ensureSession(
      baseUrl: 'http://127.0.0.1:8080',
      deviceId: 'device_memory',
    );
    await service.ensureSession(
      baseUrl: 'http://127.0.0.1:8080',
      deviceId: 'device_memory',
    );

    expect(paths, [
      '/api/sessions/anonymous',
      '/api/sessions/refresh',
    ]);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('session_access_token'), isNull);
    expect(prefs.getString('session_refresh_token'), isNull);
  });

  test('discarded sessions are replaced after refresh returns unauthorized',
      () async {
    SharedPreferences.setMockInitialValues({
      'session_access_token': 'old_access',
      'session_refresh_token': 'old_refresh',
      'session_user_id': 'usr_old',
      'session_id': 'ses_old',
      'session_expires_at': DateTime.now()
          .toUtc()
          .add(const Duration(minutes: 15))
          .toIso8601String(),
    });
    final client = MockClient((request) async {
      if (request.url.path == '/api/sessions/refresh') {
        return http.Response('unauthorized', 401);
      }
      return http.Response(
        jsonEncode({
          'userId': 'usr_new',
          'sessionId': 'ses_new',
          'accessToken': 'new_access',
          'refreshToken': 'new_refresh',
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(minutes: 15))
              .toIso8601String(),
        }),
        201,
      );
    });
    final credentials = await SessionService(
      client: client,
      secretStorage: MemorySessionSecretStore({
        'session_access_token': 'old_access',
        'session_refresh_token': 'old_refresh',
      }),
    ).ensureSession(baseUrl: 'https://qiuqiu.example', deviceId: 'device_123');

    expect(credentials.userId, 'usr_new');
    expect(credentials.accessToken, 'new_access');
  });
}

class MemorySessionSecretStore implements SessionSecretStore {
  final Map<String, String> values;

  MemorySessionSecretStore([Map<String, String>? values])
      : values = values ?? <String, String>{};

  @override
  Future<String?> read(String key) async => values[key];

  @override
  Future<void> write(String key, String value) async => values[key] = value;

  @override
  Future<void> delete(String key) async => values.remove(key);
}

class ThrowingSessionSecretStore implements SessionSecretStore {
  @override
  Future<String?> read(String key) => throw StateError('storage unavailable');

  @override
  Future<void> write(String key, String value) =>
      throw StateError('storage unavailable');

  @override
  Future<void> delete(String key) => throw StateError('storage unavailable');
}
