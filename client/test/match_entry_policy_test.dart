import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_screen.dart';

void main() {
  test('catalog selections never auto-enter the companion experience', () {
    // 目录点选只打开比赛数据视图：MatchEntryScreen 固定传 autoEnter:false
    // （原 shouldAutoEnterMatch 恒 false 死标志已内联删除）；进入陪看必须
    // 是用户的显式动作，因此 MatchScreen 默认绝不自动进入。
    expect(const MatchScreen(matchId: 'any').autoEnter, isFalse);
  });
}
