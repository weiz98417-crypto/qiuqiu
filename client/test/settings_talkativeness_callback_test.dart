import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/settings_screen.dart';
import 'package:qiuqiu/services/preferences_service.dart';

/// 话痨档位回调锁定（快修 P1：onTalkativenessChanged 此前无人传，
/// set_talkativeness 即时持久化断线）。设置页挂在垫底页之上——保存后
/// Navigator.pop 回垫底页，history 不空。
void main() {
  Future<void> openSettings(
    WidgetTester tester, {
    required UserProfile initial,
    required ValueChanged<String>? onTalkativenessChanged,
  }) async {
    // 拉高窗口：保存按钮在懒加载 ListView 低位，默认视口不构建。
    tester.view.physicalSize = const Size(1080, 3200);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => Navigator.push(
              context,
              MaterialPageRoute(
                builder: (_) => SettingsScreen(
                  initialProfile: initial,
                  onSave: (_) async {},
                  onTalkativenessChanged: onTalkativenessChanged,
                ),
              ),
            ),
            child: const Text('打开设置'),
          ),
        ),
      ),
    );
    await tester.tap(find.text('打开设置'));
    await tester.pumpAndSettle();
  }

  testWidgets('改档后保存：回调带新档位触发一次，并 pop 回垫底页', (tester) async {
    final changedTiers = <String>[];
    await openSettings(
      tester,
      initial: const UserProfile(
        nickname: '老张',
        favoriteTeam: '曼联',
        talkativeness: 'normal',
      ),
      onTalkativenessChanged: changedTiers.add,
    );

    await tester.tap(find.text('热闹'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, '保存陪看偏好'));
    await tester.pumpAndSettle();

    expect(changedTiers, ['active']);
    expect(find.text('打开设置'), findsOneWidget);
  });

  testWidgets('档位未变时保存：不触发回调', (tester) async {
    final changedTiers = <String>[];
    await openSettings(
      tester,
      initial: const UserProfile(
        nickname: '老张',
        favoriteTeam: '曼联',
        talkativeness: 'normal',
      ),
      onTalkativenessChanged: changedTiers.add,
    );

    await tester.tap(find.widgetWithText(FilledButton, '保存陪看偏好'));
    await tester.pumpAndSettle();

    expect(changedTiers, isEmpty);
  });
}
