import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';

import '../services/match_session_controller.dart';
import '../theme/app_theme.dart';
class ConversationDock extends StatelessWidget {
  final MatchSessionPhase phase;
  final bool continuousEnabled;
  final String userLine;
  final String? notice;
  final bool textMode;
  final TextEditingController textController;
  final VoidCallback onToggleContinuous;
  final String audioInputLabel;
  final VoidCallback onChooseAudioInput;
  final VoidCallback onOpenSettings;
  final VoidCallback onOpenText;
  final VoidCallback onCloseText;
  final VoidCallback onSendText;
  final Future<void> Function() onMicDown;
  final VoidCallback onMicUp;

  const ConversationDock({
    required this.phase,
    required this.continuousEnabled,
    required this.userLine,
    required this.notice,
    required this.textMode,
    required this.textController,
    required this.onToggleContinuous,
    required this.audioInputLabel,
    required this.onChooseAudioInput,
    required this.onOpenSettings,
    required this.onOpenText,
    required this.onCloseText,
    required this.onSendText,
    required this.onMicDown,
    required this.onMicUp,
  });

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: const Color(0xB30B2E68),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.md,
          AppSpacing.sm,
          AppSpacing.md,
          AppSpacing.md,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: [
            Row(
              children: [
                Container(
                  width: 8,
                  height: 8,
                  decoration: BoxDecoration(
                    color: _phaseColor,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: AppSpacing.xs),
                Expanded(
                  child: Text(
                    _phaseLabel,
                    style: Theme.of(context).textTheme.labelMedium?.copyWith(
                          color: AppColors.ink,
                        ),
                  ),
                ),
                const SizedBox(width: AppSpacing.xs),
                Tooltip(
                  message: '选择语音输入',
                  child: InkWell(
                    onTap: onChooseAudioInput,
                    child: Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: AppSpacing.xs,
                        vertical: AppSpacing.xxs,
                      ),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          const Icon(
                            Icons.mic_external_on_outlined,
                            size: 17,
                            color: AppColors.ink,
                          ),
                          const SizedBox(width: AppSpacing.xxs),
                          ConstrainedBox(
                            constraints: const BoxConstraints(maxWidth: 132),
                            child: Text(
                              audioInputLabel,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context)
                                  .textTheme
                                  .labelSmall
                                  ?.copyWith(color: AppColors.ink),
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                ),
              ],
            ),
            if (notice != null) ...[
              const SizedBox(height: AppSpacing.xs),
              Text(
                notice!,
                style: Theme.of(
                  context,
                ).textTheme.bodyMedium?.copyWith(color: AppColors.ink),
              ),
            ] else if (userLine.isNotEmpty) ...[
              const SizedBox(height: AppSpacing.xs),
              Text(
                '「$userLine」',
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                      color: AppColors.ink,
                      fontWeight: FontWeight.w600,
                    ),
              ),
            ],
            const SizedBox(height: AppSpacing.sm),
            const Divider(height: 1, color: AppColors.skyBlue),
            const SizedBox(height: AppSpacing.sm),
            if (textMode)
              _TextComposer(
                controller: textController,
                onClose: onCloseText,
                onSend: onSendText,
              )
            else
              Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  _DockAction(
                    label: continuousEnabled ? '连续 · 开' : '连续 · 关',
                    onPressed: onToggleContinuous,
                  ),
                  Listener(
                    onPointerDown: (_) => onMicDown(),
                    onPointerUp: (_) => onMicUp(),
                    onPointerCancel: (_) => onMicUp(),
                    child: _VoiceOrb(
                      phase: phase,
                      continuousEnabled: continuousEnabled,
                    ),
                  ),
                  PopupMenuButton<String>(
                    tooltip: '更多陪看方式',
                    constraints: const BoxConstraints(minWidth: 160),
                    onSelected: (value) {
                      if (value == 'text') onOpenText();
                      if (value == 'settings') onOpenSettings();
                    },
                    itemBuilder: (_) => const [
                      PopupMenuItem(value: 'text', child: Text('改用文字说')),
                      PopupMenuItem(value: 'settings', child: Text('陪看设置')),
                    ],
                    child: const SizedBox(
                      width: 48,
                      height: 48,
                      child: Center(
                        child: Icon(
                          Icons.more_horiz_rounded,
                          color: AppColors.ink,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }

  Color get _phaseColor {
    switch (phase) {
      case MatchSessionPhase.welcoming:
        return AppColors.championBlue;
      case MatchSessionPhase.listening:
        return AppColors.yellow;
      case MatchSessionPhase.userSpeaking:
        return AppColors.green;
      case MatchSessionPhase.understanding:
        return AppColors.yellow;
      case MatchSessionPhase.speaking:
        return AppColors.championBlue;
      case MatchSessionPhase.permissionDenied:
      case MatchSessionPhase.offline:
      case MatchSessionPhase.failed:
        return AppColors.red;
      case MatchSessionPhase.reconnecting:
      case MatchSessionPhase.recovered:
      case MatchSessionPhase.idle:
        return AppColors.muted;
    }
  }

  String get _phaseLabel {
    switch (phase) {
      case MatchSessionPhase.welcoming:
        return '第一次见面 · 球球正在和你打招呼';
      case MatchSessionPhase.listening:
        return continuousEnabled ? '连续对话已开启 · 你直接说' : '正在听你说';
      case MatchSessionPhase.userSpeaking:
        return '听到你在说话 · 继续说';
      case MatchSessionPhase.understanding:
        return '听见了 · 正在结合比赛想一想';
      case MatchSessionPhase.speaking:
        return '球球正在回答 · 你可以随时插话';
      case MatchSessionPhase.permissionDenied:
        return '麦克风还没有权限';
      case MatchSessionPhase.offline:
        return '暂时离线 · 字幕和比赛画面仍保留';
      case MatchSessionPhase.failed:
        return '这一句没有听清';
      case MatchSessionPhase.reconnecting:
        return '重连中';
      case MatchSessionPhase.recovered:
        return '已恢复';
      case MatchSessionPhase.idle:
        return continuousEnabled ? '准备好后直接说' : '按住麦克风说话';
    }
  }
}

class _VoiceOrb extends StatelessWidget {
  final MatchSessionPhase phase;
  final bool continuousEnabled;

  const _VoiceOrb({required this.phase, required this.continuousEnabled});

  @override
  Widget build(BuildContext context) {
    final gradientColors = switch (phase) {
      MatchSessionPhase.listening || MatchSessionPhase.reconnecting => const [
          Color(0xFFFFF2A8),
          AppColors.yellow,
          Color(0xFFC68A18),
        ],
      MatchSessionPhase.userSpeaking => const [
          Color(0xFFB8F4D0),
          AppColors.green,
          Color(0xFF17865A),
        ],
      MatchSessionPhase.understanding => const [
          Color(0xFFC4E5FF),
          AppColors.skyBlue,
          Color(0xFF1E6DB8),
        ],
      MatchSessionPhase.permissionDenied ||
      MatchSessionPhase.offline ||
      MatchSessionPhase.failed =>
        const [
          Color(0xFFFFB7B8),
          AppColors.red,
          Color(0xFFAA2E3A),
        ],
      _ => const [
          Color(0xFF9BCBFF),
          AppColors.championBlue,
          AppColors.championBlueDeep,
        ],
    };
    final foreground =
        phase == MatchSessionPhase.listening ? AppColors.night : Colors.white;
    final label = switch (phase) {
      MatchSessionPhase.welcoming => '你好呀',
      MatchSessionPhase.listening => '聆听中',
      MatchSessionPhase.userSpeaking => '你在说',
      MatchSessionPhase.understanding => '想一想',
      MatchSessionPhase.speaking => '球球在说',
      MatchSessionPhase.permissionDenied => '没权限',
      MatchSessionPhase.offline => '离线',
      MatchSessionPhase.failed => '再试一次',
      MatchSessionPhase.reconnecting => '重连中',
      MatchSessionPhase.recovered => '已恢复',
      MatchSessionPhase.idle => continuousEnabled ? '直接说' : '按住说',
    };
    final isVoiceActive = phase == MatchSessionPhase.listening ||
        phase == MatchSessionPhase.userSpeaking;
    final orb = AnimatedContainer(
      duration: const Duration(milliseconds: 180),
      curve: Curves.easeOutQuart,
      width: 80,
      height: 80,
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          stops: const [0, 0.42, 1],
          colors: gradientColors,
        ),
        shape: BoxShape.circle,
        border: Border.all(
          color: AppColors.ink.withValues(alpha: 0.9),
          width: 2,
        ),
        boxShadow: [
          const BoxShadow(
            color: AppColors.night,
            offset: Offset(4, 5),
            blurRadius: 1,
          ),
          BoxShadow(
            color: gradientColors.last.withValues(alpha: 0.55),
            offset: const Offset(0, 5),
            blurRadius: 10,
          ),
        ],
      ),
      alignment: Alignment.center,
      child: Stack(
        alignment: Alignment.center,
        children: [
          Positioned(
            top: 10,
            left: 16,
            right: 16,
            child: IgnorePointer(
              child: Container(
                height: 8,
                decoration: BoxDecoration(
                  borderRadius: BorderRadius.circular(99),
                  gradient: LinearGradient(
                    colors: [
                      AppColors.ink.withValues(alpha: 0.48),
                      AppColors.ink.withValues(alpha: 0),
                    ],
                  ),
                ),
              ),
            ),
          ),
          Text(
            label,
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
                  color: foreground,
                  fontWeight: FontWeight.w800,
                ),
          ),
        ],
      ),
    );
    return Semantics(
      button: true,
      label: continuousEnabled ? '语音状态：$label' : '按住和球球说话',
      child: isVoiceActive
          ? orb.animate(onPlay: (controller) => controller.repeat()).shimmer(
                delay: 300.ms,
                duration: 1600.ms,
                color: Colors.white.withValues(alpha: 0.34),
                angle: 0.65,
                size: 2.6,
                blendMode: BlendMode.srcATop,
              )
          : orb,
    );
  }
}

class _DockAction extends StatelessWidget {
  final String label;
  final VoidCallback onPressed;

  const _DockAction({required this.label, required this.onPressed});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 72,
      height: 48,
      child: TextButton(
        onPressed: onPressed,
        style: TextButton.styleFrom(
          foregroundColor: AppColors.ink,
          padding: EdgeInsets.zero,
        ),
        child: Text(label),
      ),
    );
  }
}

class _TextComposer extends StatelessWidget {
  final TextEditingController controller;
  final VoidCallback onClose;
  final VoidCallback onSend;

  const _TextComposer({
    required this.controller,
    required this.onClose,
    required this.onSend,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        IconButton(
          tooltip: '回到语音',
          onPressed: onClose,
          color: AppColors.ink,
          icon: const Icon(Icons.mic_rounded),
        ),
        const SizedBox(width: AppSpacing.xs),
        Expanded(
          child: TextField(
            controller: controller,
            autofocus: true,
            style: const TextStyle(color: AppColors.ink),
            textInputAction: TextInputAction.send,
            onSubmitted: (_) => onSend(),
            decoration: const InputDecoration(
              isDense: true,
              filled: true,
              fillColor: AppColors.terrace,
              hintText: '直接和球球说…',
              hintStyle: TextStyle(color: AppColors.muted),
            ),
          ),
        ),
        const SizedBox(width: AppSpacing.xs),
        IconButton.filled(
          tooltip: '发送这句话',
          onPressed: onSend,
          icon: const Icon(Icons.arrow_upward_rounded),
        ),
      ],
    );
  }
}

