import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qiuqiu/screens/match_catalog_screen.dart';
import 'package:qiuqiu/services/match_catalog_service.dart';
import 'package:qiuqiu/theme/app_theme.dart';

void main() {
  testWidgets('user can filter and select a match from the catalog',
      (tester) async {
    final service = MatchCatalogService(
      client: MockClient((_) async => http.Response(
          '''
        {"matches":[
          {"matchId":"live-1","homeTeam":"阿森纳","awayTeam":"曼城","competition":"英超","status":"live","liveLabel":"直播中"},
          {"matchId":"next-1","homeTeam":"利物浦","awayTeam":"切尔西","competition":"英超","status":"scheduled","liveLabel":"等待开赛"}
        ]}
      ''',
          200,
          headers: {'content-type': 'application/json; charset=utf-8'})),
    );
    addTearDown(service.close);
    MatchCatalogItem? selected;

    await tester.pumpWidget(MaterialApp(
      theme: AppTheme.dark,
      home: MatchCatalogScreen(
        apiBaseUrl: 'http://example.test',
        service: service,
        onSelected: (match) => selected = match,
      ),
    ));
    await tester.pumpAndSettle();

    expect(find.text('阿森纳 vs 曼城'), findsOneWidget);
    expect(find.text('利物浦 vs 切尔西'), findsOneWidget);

    await tester.tap(find.widgetWithText(ChoiceChip, '直播中'));
    await tester.pumpAndSettle();
    expect(find.text('阿森纳 vs 曼城'), findsOneWidget);
    expect(find.text('利物浦 vs 切尔西'), findsNothing);

    await tester.tap(find.text('进入陪看'));
    expect(selected?.matchId, 'live-1');
  });

  testWidgets('scheduled matches clearly require an explicit entry',
      (tester) async {
    final service = MatchCatalogService(
      client: MockClient((_) async => http.Response(
          '{"matches":[{"matchId":"next-1","homeTeam":"利物浦","awayTeam":"切尔西","status":"scheduled","liveLabel":"等待开赛"}]}',
          200,
          headers: {'content-type': 'application/json; charset=utf-8'})),
    );
    addTearDown(service.close);

    await tester.pumpWidget(MaterialApp(
      theme: AppTheme.dark,
      home: MatchCatalogScreen(
        apiBaseUrl: 'http://example.test',
        service: service,
        onSelected: (_) {},
      ),
    ));
    await tester.pumpAndSettle();

    expect(find.text('查看赛前'), findsOneWidget);
    expect(find.text('进入陪看'), findsNothing);
  });
}
