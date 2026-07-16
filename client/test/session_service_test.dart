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
    final service = SessionService(client: client);

    final credentials = await service.ensureSession(
      baseUrl: 'ws://127.0.0.1:8080/ws/match/test',
      deviceId: 'device_123',
    );

    expect(requestCount, 1);
    expect(credentials.userId, 'usr_server');
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('session_access_token'), 'access_token');
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
}
