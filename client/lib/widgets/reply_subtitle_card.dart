import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';

import '../theme/app_theme.dart';

class ReplySubtitleCard extends StatefulWidget {
  final String primaryText;
  final String secondaryText;
  final double maxHeight;

  /// 主动回合理由(memory-surfacing 1.6):非空时在字幕顶部展示
  /// 「为什么找我聊」行——球球主动开口的因果可感。普通回合 null。
  final String? reasonText;

  const ReplySubtitleCard({
    super.key,
    required this.primaryText,
    required this.secondaryText,
    required this.maxHeight,
    this.reasonText,
  });

  @override
  State<ReplySubtitleCard> createState() => _ReplySubtitleCardState();
}

class _ReplySubtitleCardState extends State<ReplySubtitleCard> {
  final ScrollController _scrollController = ScrollController();
  bool _isScrollable = false;

  @override
  void initState() {
    super.initState();
    _scheduleOverflowCheck();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _scheduleOverflowCheck();
  }

  @override
  void didUpdateWidget(ReplySubtitleCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    final replyChanged = widget.primaryText != oldWidget.primaryText ||
        widget.secondaryText != oldWidget.secondaryText;
    if (replyChanged) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (!mounted || !_scrollController.hasClients) return;
        _scrollController.jumpTo(0);
        _updateOverflowState();
      });
      return;
    }
    if (widget.maxHeight != oldWidget.maxHeight) {
      _scheduleOverflowCheck();
    }
  }

  void _scheduleOverflowCheck() {
    WidgetsBinding.instance.addPostFrameCallback((_) => _updateOverflowState());
  }

  void _updateOverflowState() {
    if (!mounted || !_scrollController.hasClients) return;
    final isScrollable = _scrollController.position.maxScrollExtent > 0;
    if (isScrollable == _isScrollable) return;
    setState(() => _isScrollable = isScrollable);
  }

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final primaryText = widget.primaryText.trim();
    final secondaryText = widget.secondaryText.trim();
    final spokenText =
        [primaryText, secondaryText].where((part) => part.isNotEmpty).join(' ');

    return Semantics(
      liveRegion: true,
      excludeSemantics: true,
      label: '球球说：$spokenText',
      child: AnimatedSize(
        alignment: Alignment.bottomCenter,
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOutCubic,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: widget.maxHeight),
          child: Container(
            clipBehavior: Clip.antiAlias,
            decoration: BoxDecoration(
              color: AppColors.night.withValues(alpha: 0.88),
              border: Border.all(
                color: AppColors.ink.withValues(alpha: 0.14),
              ),
              borderRadius: BorderRadius.circular(10),
            ),
            child: Scrollbar(
              controller: _scrollController,
              thumbVisibility: _isScrollable,
              interactive: true,
              thickness: 3,
              radius: const Radius.circular(3),
              child: SingleChildScrollView(
                controller: _scrollController,
                primary: false,
                padding: const EdgeInsets.fromLTRB(
                  AppSpacing.md,
                  AppSpacing.sm,
                  AppSpacing.lg,
                  AppSpacing.sm,
                ),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    if ((widget.reasonText ?? '').trim().isNotEmpty) ...[
                      Padding(
                        padding: const EdgeInsets.only(bottom: AppSpacing.xs),
                        child: Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Icon(
                              Icons.auto_awesome_rounded,
                              size: 13,
                              color: AppColors.orange.withValues(alpha: 0.85),
                            ),
                            const SizedBox(width: 4),
                            Flexible(
                              child: Text(
                                (widget.reasonText ?? '').trim(),
                                softWrap: true,
                                style: Theme.of(context)
                                    .textTheme
                                    .bodySmall
                                    ?.copyWith(
                                      color: AppColors.ink.withValues(alpha: 0.65),
                                      height: 1.35,
                                    ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                    Text(
                      primaryText,
                      softWrap: true,
                      overflow: TextOverflow.visible,
                      style:
                          Theme.of(context).textTheme.headlineMedium?.copyWith(
                                fontSize: 22,
                                height: 1.32,
                                letterSpacing: -0.2,
                              ),
                    ),
                    if (secondaryText.isNotEmpty) ...[
                      const SizedBox(height: AppSpacing.xs),
                      Text(
                        secondaryText,
                        softWrap: true,
                        overflow: TextOverflow.visible,
                        style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                              color: AppColors.ink.withValues(alpha: 0.9),
                              height: 1.55,
                            ),
                      ),
                    ],
                  ],
                ),
              ),
            )
                .animate()
                .fadeIn(
                  duration: 180.ms,
                  curve: Curves.easeOutCubic,
                )
                .slideY(begin: 0.04, end: 0, duration: 220.ms),
          ),
        ),
      ),
    );
  }
}
