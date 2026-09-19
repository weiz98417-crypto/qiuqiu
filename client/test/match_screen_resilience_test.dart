import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_screen.dart';
import 'package:qiuqiu/services/websocket_service.dart';

void main() {
  group('shouldRefetchMatchOverview', () {
    test('refetches once when the transport enters connected', () {
      expect(
        shouldRefetchMatchOverview(
          status: SocketStatus.connected,
          previousStatus: SocketStatus.connecting,
        ),
        isTrue,
      );
      expect(
        shouldRefetchMatchOverview(
          status: SocketStatus.connected,
          previousStatus: SocketStatus.reconnecting,
        ),
        isTrue,
      );
      expect(
        shouldRefetchMatchOverview(
          status: SocketStatus.connected,
          previousStatus: SocketStatus.disconnected,
        ),
        isTrue,
      );
    });

    test('does not refetch outside the connected edge', () {
      expect(
        shouldRefetchMatchOverview(
          status: SocketStatus.connected,
          previousStatus: SocketStatus.connected,
        ),
        isFalse,
        reason: '重复的 connected 事件不重复拉取',
      );
      for (final status in SocketStatus.values) {
        if (status == SocketStatus.connected) continue;
        expect(
          shouldRefetchMatchOverview(
            status: status,
            previousStatus: SocketStatus.connected,
          ),
          isFalse,
          reason: '非 connected 状态不触发拉取',
        );
      }
    });
  });

  group('resolveMatchConnectionConfig', () {
    test('debug builds keep the convenient dev fallbacks', () {
      final config = resolveMatchConnectionConfig(releaseMode: false);

      expect(config.isMissing, isFalse);
      expect(config.matchId, 'test');
      expect(config.socketUrl, 'ws://10.0.2.2:8080/ws/match/test');
    });

    test(
        'release build without any configuration refuses to join the dev host',
        () {
      final config = resolveMatchConnectionConfig(releaseMode: true);

      expect(config.isMissing, isTrue);
      expect(config.missingReason, contains('QIUQIU_WS_URL'));
    });

    test('release build without a match id refuses the test-match default',
        () {
      final config = resolveMatchConnectionConfig(
        configuredSocketUrl: 'wss://qiuqiu.example.com',
        releaseMode: true,
      );

      expect(config.isMissing, isTrue);
      expect(config.missingReason, contains('比赛 ID'));
    });

    test('explicit match id and dart-define url win over every default', () {
      final config = resolveMatchConnectionConfig(
        requestedMatchId: 'el-classico',
        configuredSocketUrl: 'wss://qiuqiu.example.com/ws/match/test',
        releaseMode: true,
      );

      expect(config.isMissing, isFalse);
      expect(config.matchId, 'el-classico');
      expect(config.socketUrl, 'wss://qiuqiu.example.com/ws/match/el-classico');
    });

    test(
        'a fully configured url keeps the release build working without a separate match id',
        () {
      final config = resolveMatchConnectionConfig(
        configuredSocketUrl: 'wss://qiuqiu.example.com/ws/match/el-classico',
        releaseMode: true,
      );

      expect(config.isMissing, isFalse);
      expect(config.matchId, 'el-classico');
      expect(config.socketUrl, 'wss://qiuqiu.example.com/ws/match/el-classico');
    });

    test('web builds derive a same-origin socket url from the page', () {
      final config = resolveMatchConnectionConfig(
        isWeb: true,
        pageUri: Uri.parse('https://qiuqiu.example.com/?matchId=derby'),
        releaseMode: true,
      );

      expect(config.isMissing, isFalse);
      expect(config.matchId, 'derby');
      expect(config.socketUrl, 'wss://qiuqiu.example.com/ws/match/derby');
    });

    test('release web without any match id refuses the test-match default',
        () {
      final config = resolveMatchConnectionConfig(
        isWeb: true,
        pageUri: Uri.parse('https://qiuqiu.example.com/'),
        releaseMode: true,
      );

      expect(config.isMissing, isTrue);
    });

    test('debug web without a match id keeps the test-match default', () {
      final config = resolveMatchConnectionConfig(
        isWeb: true,
        pageUri: Uri.parse('http://localhost:7357/'),
        releaseMode: false,
      );

      expect(config.isMissing, isFalse);
      expect(config.matchId, 'test');
      expect(config.socketUrl, 'ws://localhost:7357/ws/match/test');
    });
  });

  testWidgets('missing configuration renders a clear error empty state',
      (tester) async {
    await tester.pumpWidget(const MaterialApp(
      home: MissingMatchConfigView(detail: '缺少服务地址配置（QIUQIU_WS_URL）'),
    ));

    expect(find.text('缺少比赛连接配置'), findsOneWidget);
    expect(find.textContaining('QIUQIU_WS_URL'), findsWidgets);
    expect(find.textContaining('--dart-define'), findsOneWidget);
  });
}
