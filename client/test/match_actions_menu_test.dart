import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/widgets/match_actions_menu.dart';

void main() {
  testWidgets('match menu exposes switching settings and leaving',
      (tester) async {
    MatchAction? selected;
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: MatchActionsMenu(onSelected: (action) => selected = action),
      ),
    ));

    await tester.tap(find.byTooltip('比赛选项'));
    await tester.pumpAndSettle();
    expect(find.text('切换比赛'), findsOneWidget);
    expect(find.text('陪看设置'), findsOneWidget);
    expect(find.text('退出本场'), findsOneWidget);

    await tester.tap(find.text('切换比赛'));
    await tester.pumpAndSettle();
    expect(selected, MatchAction.switchMatch);
  });

  testWidgets('exit dialog lets the user stay or leave', (tester) async {
    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => Scaffold(
          body: FilledButton(
            onPressed: () => showMatchExitDialog(context),
            child: const Text('退出'),
          ),
        ),
      ),
    ));

    await tester.tap(find.text('退出'));
    await tester.pumpAndSettle();
    expect(find.text('退出这场比赛？'), findsOneWidget);
    await tester.tap(find.text('继续陪看'));
    await tester.pumpAndSettle();
    expect(find.text('退出这场比赛？'), findsNothing);

    await tester.tap(find.text('退出'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('退出本场'));
    await tester.pumpAndSettle();
    expect(find.text('退出这场比赛？'), findsNothing);
  });

  testWidgets('switch dialog requires explicit confirmation', (tester) async {
    await tester.pumpWidget(MaterialApp(
      home: Builder(
        builder: (context) => Scaffold(
          body: FilledButton(
            onPressed: () => showMatchSwitchDialog(context),
            child: const Text('切换'),
          ),
        ),
      ),
    ));

    await tester.tap(find.text('切换'));
    await tester.pumpAndSettle();
    expect(find.text('换一场比赛？'), findsOneWidget);
    await tester.tap(find.text('留在本场'));
    await tester.pumpAndSettle();
    expect(find.text('换一场比赛？'), findsNothing);
  });
}
