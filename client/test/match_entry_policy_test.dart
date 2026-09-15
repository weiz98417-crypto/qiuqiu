import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_entry_screen.dart';
import 'package:qiuqiu/services/match_catalog_service.dart';

void main() {
  test('catalog selections never auto-enter the companion experience', () {
    MatchCatalogItem match(String status) => MatchCatalogItem(
          matchId: status,
          homeTeam: '阿森纳',
          awayTeam: '利兹联',
          status: status,
        );

    expect(shouldAutoEnterMatch(match('live')), isFalse);
    expect(shouldAutoEnterMatch(match('scheduled')), isFalse);
    expect(shouldAutoEnterMatch(match('finished')), isFalse);
  });
}
