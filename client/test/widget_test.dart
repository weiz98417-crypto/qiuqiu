import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_screen.dart';
import 'package:qiuqiu/services/preferences_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('default match card waits for real match information', () {
    const match = MatchViewData();

    expect(match.statusCarouselItems, const ['等待比赛信息']);
    expect(match.homeTeam, isNot('利物浦'));
    expect(match.awayTeam, isNot('切尔西'));
  });

  test('match snapshot updates the user-facing scoreboard', () {
    const initial = MatchViewData();

    final updated = initial.withSnapshot({
      'homeTeam': '阿森纳',
      'awayTeam': '曼城',
      'score': {'home': 1, 'away': 2},
      'period': '下半场',
      'clock': "83'",
      'recentEvents': [
        {
          'eventType': 'goal',
          'playerName': '哈兰德',
          'description': '哈兰德禁区内破门',
        },
        {
          'eventType': 'goal',
          'playerName': '旧事件',
          'description': '这条不应显示',
        },
      ],
    });

    expect(updated.homeTeam, '阿森纳');
    expect(updated.awayTeam, '曼城');
    expect(updated.homeScore, 1);
    expect(updated.awayScore, 2);
    expect(updated.clock, "83'");
    expect(updated.eventLabel, '刚刚 · 哈兰德禁区内破门');
    expect(updated.recentEventLabels, hasLength(2));
    expect(updated.statusCarouselItems, hasLength(4));
  });

  test('snapshot without events clears the demo goal from the carousel', () {
    const initial = MatchViewData();

    final updated = initial.withSnapshot({
      'homeTeam': '利物浦',
      'awayTeam': '切尔西',
      'score': {'home': 0, 'away': 0},
      'period': 'pre_match',
      'clock': "1'",
      'recentEvents': <Map<String, dynamic>>[],
    });

    expect(updated.recentEventLabels, isEmpty);
    expect(
      updated.statusCarouselItems.any((item) => item.contains('萨拉赫')),
      isFalse,
    );
    expect(updated.eventLabel, contains('0—0'));
    expect(updated.eventLabel, startsWith('赛前'));
    expect(updated.liveLabel, '等待开赛');
  });

  test('profile preferences preserve the continuous conversation choice', () {
    const profile = UserProfile(
      nickname: '小林',
      favoriteTeam: '利物浦',
      talkativeness: 'normal',
    );

    final updated = profile.copyWith(
      continuousConversation: false,
      subtitlesEnabled: false,
      soundEnabled: true,
    );

    expect(updated.nickname, '小林');
    expect(updated.continuousConversation, isFalse);
    expect(updated.subtitlesEnabled, isFalse);
    expect(updated.soundEnabled, isTrue);
  });

  test('first meeting is only marked complete after the greeting', () async {
    SharedPreferences.setMockInitialValues({});
    final preferences = PreferencesService();

    expect(await preferences.hasCompletedFirstMeeting(), isFalse);

    await preferences.markFirstMeetingCompleted();

    expect(await preferences.hasCompletedFirstMeeting(), isTrue);
  });
}
