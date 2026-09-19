import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_dock.dart';
import 'package:qiuqiu/services/match_session_controller.dart';

void main() {
  Widget dock({
    MatchSessionPhase phase = MatchSessionPhase.listening,
    bool continuousEnabled = true,
    required Future<void> Function() onMicDown,
    required VoidCallback onMicUp,
    required VoidCallback onToggleContinuous,
  }) {
    return MaterialApp(
      home: Scaffold(
        body: ConversationDock(
          phase: phase,
          continuousEnabled: continuousEnabled,
          userLine: '',
          notice: null,
          textMode: false,
          textController: TextEditingController(),
          onToggleContinuous: onToggleContinuous,
          audioInputLabel: '选择语音输入',
          onChooseAudioInput: () {},
          onOpenSettings: () {},
          onOpenText: () {},
          onCloseText: () {},
          onSendText: () {},
          onMicDown: onMicDown,
          onMicUp: onMicUp,
        ),
      ),
    );
  }

  testWidgets('shows the phase label for the current session phase', (tester) async {
    await tester.pumpWidget(dock(
      phase: MatchSessionPhase.speaking,
      onMicDown: () async {},
      onMicUp: () {},
      onToggleContinuous: () {},
    ));
    expect(find.text('球球正在回答 · 你可以随时插话'), findsOneWidget);
  });

  testWidgets('continuous toggle forwards the tap', (tester) async {
    var toggles = 0;
    await tester.pumpWidget(dock(
      continuousEnabled: false,
      onMicDown: () async {},
      onMicUp: () {},
      onToggleContinuous: () => toggles += 1,
    ));
    await tester.tap(find.text('连续 · 关'));
    await tester.pump();
    expect(toggles, 1);
    // 冲掉 widget 内部的一次性计时器，避免测试退出时报 pending timer。
    await tester.pump(const Duration(seconds: 30));
  });
}
