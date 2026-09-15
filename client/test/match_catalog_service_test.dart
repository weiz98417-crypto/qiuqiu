import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qiuqiu/services/match_catalog_service.dart';

void main() {
  test('catalog parses and filters invalid match entries', () async {
    final client = MockClient((request) async {
      expect(request.url.path, '/api/matches/catalog');
      return http.Response(
        jsonEncode({
          'matches': [
            {
              'matchId': 'm1',
              'homeTeam': '阿森纳',
              'awayTeam': '曼城',
              'status': 'live',
              'liveLabel': '直播中',
              'homeScore': 1,
              'awayScore': 0,
            },
            {'homeTeam': '缺少 ID'},
          ],
        }),
        200,
        headers: {'content-type': 'application/json'},
      );
    });
    final matches = await MatchCatalogService(client: client)
        .fetch('http://localhost:8080');
    expect(matches, hasLength(1));
    expect(matches.single.title, '阿森纳 vs 曼城');
    expect(matches.single.liveLabel, '直播中');
  });

  test('catalog reports server failures', () async {
    final service = MatchCatalogService(
      client: MockClient((_) async => http.Response('bad', 503)),
    );
    expect(
      () => service.fetch('http://localhost:8080'),
      throwsA(isA<MatchCatalogException>()),
    );
  });

  test('catalog localizes known provider names for match cards', () async {
    final service = MatchCatalogService(
      client: MockClient((_) async => http.Response(
            jsonEncode({
              'matches': [
                {
                  'matchId': 'world-cup',
                  'homeTeam': 'England',
                  'awayTeam': 'Congo DR',
                  'competition': '2026 FIFA World Cup',
                  'status': 'finished',
                },
              ],
            }),
            200,
            headers: {'content-type': 'application/json'},
          )),
    );
    addTearDown(service.close);

    final matches = await service.fetch('http://localhost:8080');

    expect(matches.single.title, '英格兰 vs 刚果（金）');
    expect(matches.single.competition, '2026 国际足联世界杯');
  });
}
