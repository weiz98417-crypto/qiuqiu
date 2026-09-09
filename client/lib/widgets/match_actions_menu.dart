import 'package:flutter/material.dart';

enum MatchAction { switchMatch, settings, leave }

Future<bool?> showMatchExitDialog(BuildContext context) {
  return showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: const Text('退出这场比赛？'),
      content: const Text('球球会停止本场的麦克风和语音播放，你可以随时从比赛列表回来。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(dialogContext, false),
          child: const Text('继续陪看'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(dialogContext, true),
          child: const Text('退出本场'),
        ),
      ],
    ),
  );
}

Future<bool?> showMatchSwitchDialog(BuildContext context) {
  return showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: const Text('换一场比赛？'),
      content: const Text('当前陪看会先暂停并回到比赛列表，之后可以随时回来。'),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(dialogContext, false),
          child: const Text('留在本场'),
        ),
        FilledButton(
          onPressed: () => Navigator.pop(dialogContext, true),
          child: const Text('切换比赛'),
        ),
      ],
    ),
  );
}

class MatchActionsMenu extends StatelessWidget {
  final ValueChanged<MatchAction> onSelected;

  const MatchActionsMenu({super.key, required this.onSelected});

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<MatchAction>(
      tooltip: '比赛选项',
      icon: const Icon(Icons.more_horiz_rounded),
      onSelected: onSelected,
      itemBuilder: (context) => const [
        PopupMenuItem(
          value: MatchAction.switchMatch,
          child: ListTile(
            leading: Icon(Icons.swap_horiz_rounded),
            title: Text('切换比赛'),
          ),
        ),
        PopupMenuItem(
          value: MatchAction.settings,
          child: ListTile(
            leading: Icon(Icons.tune_rounded),
            title: Text('陪看设置'),
          ),
        ),
        PopupMenuItem(
          value: MatchAction.leave,
          child: ListTile(
            leading: Icon(Icons.exit_to_app_rounded),
            title: Text('退出本场'),
          ),
        ),
      ],
    );
  }
}
