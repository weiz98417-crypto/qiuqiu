import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/theme/app_theme.dart';
import 'package:qiuqiu/widgets/reply_subtitle_card.dart';

void main() {
  testWidgets('long subtitles keep the complete reply and can be scrolled', (
    tester,
  ) async {
    const primaryText = '今天有一场很值得看的比赛。';
    const secondaryText = '西班牙正在对阵德国，目前西班牙一比零领先。比赛节奏很快，双方都在持续制造机会。'
        '如果你愿意，我可以继续陪你看，并在进球、红牌或者比赛结束时及时告诉你。';

    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.dark,
        home: const Scaffold(
          body: Center(
            child: SizedBox(
              width: 320,
              child: ReplySubtitleCard(
                primaryText: primaryText,
                secondaryText: secondaryText,
                maxHeight: 150,
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text(primaryText), findsOneWidget);
    expect(find.text(secondaryText), findsOneWidget);

    final primary = tester.widget<Text>(find.text(primaryText));
    final secondary = tester.widget<Text>(find.text(secondaryText));
    expect(primary.maxLines, isNull);
    expect(primary.overflow, TextOverflow.visible);
    expect(secondary.maxLines, isNull);
    expect(secondary.overflow, TextOverflow.visible);

    final scrollView = tester.widget<SingleChildScrollView>(
      find.byType(SingleChildScrollView),
    );
    expect(scrollView.controller, isNotNull);
    expect(scrollView.controller!.position.maxScrollExtent, greaterThan(0));

    await tester.drag(find.byType(SingleChildScrollView), const Offset(0, -80));
    await tester.pump();
    expect(scrollView.controller!.offset, greaterThan(0));
    expect(tester.takeException(), isNull);
  });

  testWidgets('short subtitles stay compact without becoming scrollable', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.dark,
        home: const Scaffold(
          body: Center(
            child: SizedBox(
              width: 320,
              child: ReplySubtitleCard(
                primaryText: '听到了。',
                secondaryText: '',
                maxHeight: 180,
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final scrollView = tester.widget<SingleChildScrollView>(
      find.byType(SingleChildScrollView),
    );
    expect(scrollView.controller!.position.maxScrollExtent, 0);
    expect(tester.takeException(), isNull);
  });
}
