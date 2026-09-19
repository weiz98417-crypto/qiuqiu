import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_lobby.dart';
import 'package:qiuqiu/services/match_view_data.dart';
import 'package:qiuqiu/services/match_overview_service.dart';
import 'package:qiuqiu/services/websocket_service.dart';
import 'package:qiuqiu/widgets/metal_button.dart';

void main() {
  Widget lobby({
    VoidCallback? onEnter,
    String? overviewError,
  }) {
    return MaterialApp(
      home: Scaffold(
        body: MatchLobby(
          match: const MatchViewData(
            homeTeam: '西班牙',
            awayTeam: '德国',
            homeScore: 1,
            awayScore: 0,
            hasMatchInfo: true,
          ),
          overview: const MatchOverviewData(
            homeTeam: '西班牙',
            awayTeam: '德国',
            homeScore: 1,
            awayScore: 0,
          ),
          overviewLoading: false,
          overviewError: overviewError,
          socketStatus: SocketStatus.connected,
          onEnter: onEnter ?? () {},
          onReconnect: () {},
          onOpenSettings: () {},
          onLeave: () {},
        ),
      ),
    );
  }

  testWidgets('shows the fixture and live score', (tester) async {
    await tester.pumpWidget(lobby());
    expect(find.text('西班牙'), findsWidgets);
    expect(find.text('德国'), findsWidgets);
  });

  testWidgets('enter button invokes onEnter', (tester) async {
    var enters = 0;
    await tester.pumpWidget(lobby(onEnter: () => enters += 1));
    await tester.tap(find.byType(MetalButton).first);
    await tester.pump();
    expect(enters, 1);
  });

  testWidgets('overview error keeps the live-score entry available', (tester) async {
    await tester.pumpWidget(lobby(overviewError: '详细资料暂时不可用'));
    expect(find.textContaining('详细资料暂时不可用'), findsWidgets);
  });
}
