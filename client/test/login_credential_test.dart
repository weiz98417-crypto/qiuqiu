import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qiuqiu/screens/login_screen.dart';
import 'package:qiuqiu/services/session_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'session_service_test.dart' show MemorySessionSecretStore;

SessionCredentials _credentials({String userId = 'usr_server'}) {
  return SessionCredentials(
    userId: userId,
    sessionId: 'ses_server',
    accessToken: 'access_token',
    refreshToken: 'refresh_token',
    expiresAt: DateTime.now().toUtc().add(const Duration(minutes: 15)),
  );
}

Map<String, String> _headers(http.Request request) => {
      if (request.headers['authorization'] != null)
        'authorization': request.headers['authorization']!,
    };

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('login posts the bearer session and persists returned credentials',
      () async {
    SharedPreferences.setMockInitialValues({});
    final requests = <http.Request>[];
    final client = MockClient((request) async {
      requests.add(request);
      if (request.url.path == '/api/sessions/anonymous') {
        return http.Response(
          jsonEncode({
            'userId': 'usr_anon',
            'sessionId': 'ses_anon',
            'accessToken': 'anon_access',
            'refreshToken': 'anon_refresh',
            'expiresAt': DateTime.now()
                .toUtc()
                .add(const Duration(minutes: 15))
                .toIso8601String(),
          }),
          201,
        );
      }
      expect(request.url.path, '/api/sessions/login');
      expect(_headers(request)['authorization'], 'Bearer anon_access');
      expect(jsonDecode(request.body), {
        'identifier': 'Fan@Example.com',
        'password': 'password123',
      });
      return http.Response(
        jsonEncode({
          'userId': 'usr_anon',
          'sessionId': 'ses_after_login',
          'accessToken': 'login_access',
          'refreshToken': 'login_refresh',
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(minutes: 15))
              .toIso8601String(),
        }),
        200,
      );
    });
    final service = SessionService(
      client: client,
      secretStorage: MemorySessionSecretStore(),
    );

    final credentials = await service.login(
      baseUrl: 'http://127.0.0.1:8080',
      deviceId: 'device_123',
      identifier: 'Fan@Example.com',
      password: 'password123',
    );

    expect(credentials.userId, 'usr_anon');
    expect(credentials.refreshToken, 'login_refresh');
    expect(await service.savedIdentifier(), 'fan@example.com');
  });

  test('login surfaces 401 as a SessionException and keeps the old state',
      () async {
    SharedPreferences.setMockInitialValues({});
    final client = MockClient((request) async {
      if (request.url.path == '/api/sessions/anonymous') {
        return http.Response(
          jsonEncode({
            'userId': 'usr_anon',
            'sessionId': 'ses_anon',
            'accessToken': 'anon_access',
            'refreshToken': 'anon_refresh',
            'expiresAt': DateTime.now()
                .toUtc()
                .add(const Duration(minutes: 15))
                .toIso8601String(),
          }),
          201,
        );
      }
      return http.Response('invalid identifier or password', 401);
    });
    final service = SessionService(
      client: client,
      secretStorage: MemorySessionSecretStore(),
    );

    await expectLater(
      service.login(
        baseUrl: 'http://127.0.0.1:8080',
        deviceId: 'device_123',
        identifier: 'fan@example.com',
        password: 'wrong-password',
      ),
      throwsA(
        isA<SessionException>().having(
          (error) => error.statusCode,
          'statusCode',
          401,
        ),
      ),
    );
    expect(await service.savedIdentifier(), isNull);
  });

  testWidgets('login screen gates the submit button and renders the form',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final client = MockClient((request) async {
      return http.Response(
        jsonEncode({
          'userId': 'usr_anon',
          'sessionId': 'ses_after',
          'accessToken': 'access',
          'refreshToken': 'refresh',
          'expiresAt': DateTime.now()
              .toUtc()
              .add(const Duration(minutes: 15))
              .toIso8601String(),
        }),
        200,
      );
    });
    final service = SessionService(
      client: client,
      secretStorage: MemorySessionSecretStore(),
    );

    await tester.pumpWidget(
      MaterialApp(
        home: LoginScreen(
          sessions: service,
          baseUrl: 'http://127.0.0.1:8080',
          deviceId: 'device_123',
        ),
      ),
    );

    // 未填表单、未勾协议：按钮禁用。
    final buttonFinder = find.widgetWithText(FilledButton, '登录 / 注册');
    expect(tester.widget<FilledButton>(buttonFinder).onPressed, isNull);

    await tester.enterText(
      find.widgetWithText(TextField, '邮箱'),
      'fan@example.com',
    );
    await tester.enterText(
      find.widgetWithText(TextField, '密码（至少 8 位）'),
      'password123',
    );
    await tester.tap(find.byType(CheckboxListTile));
    await tester.pump();
    expect(tester.widget<FilledButton>(buttonFinder).onPressed, isNotNull);
  });

  test('logout revokes and clears the stored credentials', () async {
    SharedPreferences.setMockInitialValues({
      'session_login_identifier': 'fan@example.com',
      'session_user_id': 'usr_me',
      'session_id': 'ses_me',
    });
    final seen = <String>[];
    final client = MockClient((request) async {
      seen.add(request.url.path);
      if (request.url.path == '/api/sessions/revoke') {
        expect(jsonDecode(request.body), {'refreshToken': 'login_refresh'});
        return http.Response('{"ok":true}', 200);
      }
      return http.Response('{}', 404);
    });
    final secretStorage = MemorySessionSecretStore();
    await secretStorage.write('session_access_token', 'access');
    await secretStorage.write('session_refresh_token', 'login_refresh');
    final service = SessionService(
      client: client,
      secretStorage: secretStorage,
    );

    await service.logout(baseUrl: 'http://127.0.0.1:8080');

    expect(seen, contains('/api/sessions/revoke'));
    expect(await secretStorage.read('session_access_token'), isNull);
    expect(await service.savedIdentifier(), isNull);
  });
}
