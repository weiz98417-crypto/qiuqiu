import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:qiuqiu/services/match_overview_service.dart';

void main() {
  test('overview parses match metadata, lineups, cards, and events', () async {
    final client = MockClient((request) async {
      if (request.url.path.endsWith('/config')) {
        return http.Response(
          jsonEncode({
            'config': {
              'matchId': 'demo-1',
              'homeTeam': '西班牙',
              'awayTeam': '德国',
              'competition': '欧洲杯',
              'kickoff': '2026-09-09T20:00:00+08:00',
              'round': '小组赛第 1 轮',
              'venue': '演示球场',
              'referee': '安东尼·泰勒',
              'homeCoach': '路易斯·德拉富恩特',
              'awayCoach': '朱利安·纳格尔斯曼',
              'homeFormation': '4-3-3',
              'awayFormation': '4-2-3-1',
              'homePlayers': [
                {'name': '佩德里', 'number': 8, 'position': 'MF'},
                {'name': '替补球员', 'number': 12, 'lineup': 'bench'},
              ],
              'awayPlayers': [
                {'name': '穆西亚拉', 'number': 10, 'position': 'MF'},
              ],
            },
            'snapshot': {
              'matchId': 'demo-1',
              'score': {'home': 1, 'away': 0},
              'period': 'first_half',
              'clock': '24:10',
              'stats': {'possessionHome': 54},
            },
          }),
          200,
          headers: {'content-type': 'application/json'},
        );
      }
      expect(request.url.path.endsWith('/events'), isTrue);
      return http.Response(
        jsonEncode({
          'events': [
            {
              'id': 'goal-1',
              'clock': '24:10',
              'period': 'first_half',
              'eventType': 'goal',
              'teamId': 'esp',
              'teamName': '西班牙',
              'playerName': '佩德里',
              'description': '佩德里破门',
              'score': {'home': 1, 'away': 0},
            },
            {
              'id': 'card-1',
              'clock': '18:02',
              'eventType': 'yellow_card',
              'teamId': 'ger',
              'playerName': '吕迪格',
            },
          ],
        }),
        200,
        headers: {'content-type': 'application/json'},
      );
    });

    final overview = await MatchOverviewService(client: client).fetch(
          baseUrl: 'http://localhost:8080/',
          matchId: 'demo-1',
        );

    expect(overview.matchId, 'demo-1');
    expect(overview.homeTeam, '西班牙');
    expect(overview.awayTeam, '德国');
    expect(overview.round, '小组赛第 1 轮');
    expect(overview.homeCoach, '路易斯·德拉富恩特');
    expect(overview.awayFormation, '4-2-3-1');
    expect(overview.homeStarters.single.name, '佩德里');
    expect(overview.homeBench.single.name, '替补球员');
    expect(overview.homeScore, 1);
    expect(overview.awayScore, 0);
    expect(overview.period, 'first_half');
    expect(overview.clock, '24:10');
    expect(overview.liveLabel, '直播中');
    expect(overview.cardCount('ger', 'yellow_card'), 1);
    expect(overview.events.map((event) => event.id), ['goal-1', 'card-1']);
    expect(overview.events.first.label, '佩德里破门');
    expect(overview.stats['possessionHome'], 54);
  });

  test('overview falls back to snapshot events and merges websocket updates', () {
    final overview = MatchOverviewData.fromPayload(
      config: {
        'matchId': 'demo-2',
        'homePlayers': [
          {'name': '首发', 'lineup': 'starter'},
        ],
      },
      snapshot: {
        'score': {'home': 0, 'away': 0},
        'period': 'pre_match',
        'recentEvents': [
          {
            'id': 'kickoff-1',
            'eventType': 'kickoff',
            'clock': '00:00',
          },
        ],
      },
      events: const [],
    );

    expect(overview.events.single.id, 'kickoff-1');
    final updated = overview.withSnapshot({
      'score': {'home': 1, 'away': 0},
      'period': 'second_half',
      'clock': '52:03',
      'recentEvents': [
        {
          'id': 'goal-2',
          'eventType': 'goal',
          'clock': '52:03',
          'playerName': '新进球者',
        },
        {
          'id': 'kickoff-1',
          'eventType': 'kickoff',
          'clock': '00:00',
        },
      ],
    });

    expect(updated.homeScore, 1);
    expect(updated.liveLabel, '直播中');
    expect(updated.clock, '52:03');
    expect(updated.events, hasLength(2));
    expect(updated.events.map((event) => event.id), ['kickoff-1', 'goal-2']);
  });

  test('overview reads persisted home-away technical statistics', () {
    final overview = MatchOverviewData.fromPayload(
      config: {
        'homeTeam': '阿森纳',
        'awayTeam': '考文垂城',
        'stats': [
          {
            'key': 'possessionPct',
            'label': '控球率',
            'home': 61.2,
            'away': 38.8,
            'unit': '%',
          },
        ],
      },
      snapshot: const {},
      events: const [],
    );

    expect(overview.teamStats, hasLength(1));
    expect(overview.teamStats.single.label, '控球率');
    expect(overview.teamStats.single.home, 61.2);
    expect(overview.teamStats.single.away, 38.8);
    expect(overview.teamStats.single.unit, '%');
  });

  test('overview reports config failures', () async {
    final service = MatchOverviewService(
      client: MockClient((_) async => http.Response('bad', 503)),
    );

    expect(
      () => service.fetch(baseUrl: 'http://localhost:8080', matchId: 'missing'),
      throwsA(isA<MatchOverviewException>()),
    );
  });
}
