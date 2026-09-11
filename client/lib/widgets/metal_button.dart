import 'package:flutter/material.dart';
import 'package:flutter_animate/flutter_animate.dart';

import '../theme/app_theme.dart';

class MetalButton extends StatefulWidget {
  final VoidCallback? onPressed;
  final Widget child;
  final Widget? leading;
  final bool compact;
  final bool animateEntrance;

  const MetalButton({
    super.key,
    required this.onPressed,
    required this.child,
    this.leading,
    this.compact = false,
    this.animateEntrance = false,
  });

  @override
  State<MetalButton> createState() => _MetalButtonState();
}

class _MetalButtonState extends State<MetalButton> {
  bool _pressed = false;

  @override
  Widget build(BuildContext context) {
    final enabled = widget.onPressed != null;
    final content = AnimatedScale(
      scale: _pressed ? 0.97 : 1,
      duration: const Duration(milliseconds: 100),
      curve: Curves.easeOut,
      child: Semantics(
        button: true,
        enabled: enabled,
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            onTap: enabled ? widget.onPressed : null,
            onHighlightChanged: (value) => setState(() => _pressed = value),
            borderRadius: BorderRadius.circular(12),
            child: Ink(
              height: widget.compact ? 44 : 52,
              padding: EdgeInsets.symmetric(
                horizontal: widget.compact ? 14 : 18,
              ),
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topCenter,
                  end: Alignment.bottomCenter,
                  colors: enabled
                      ? const [
                          Color(0xFF5CA9FF),
                          AppColors.championBlue,
                          AppColors.championBlueDeep,
                        ]
                      : const [Color(0xFF526174), Color(0xFF2B3544)],
                ),
                border: Border.all(
                  color: enabled
                      ? AppColors.skyBlue.withValues(alpha: 0.9)
                      : AppColors.muted.withValues(alpha: 0.35),
                ),
                borderRadius: BorderRadius.circular(12),
                boxShadow: [
                  BoxShadow(
                    color: AppColors.night.withValues(alpha: 0.45),
                    blurRadius: 8,
                    offset: const Offset(0, 4),
                  ),
                  if (enabled)
                    BoxShadow(
                      color: AppColors.championBlue.withValues(alpha: 0.24),
                      blurRadius: 16,
                    ),
                ],
              ),
              child: Row(
                mainAxisSize: MainAxisSize.min,
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  if (widget.leading != null) ...[
                    IconTheme(
                      data: const IconThemeData(color: Colors.white, size: 18),
                      child: widget.leading!,
                    ),
                    const SizedBox(width: 8),
                  ],
                  DefaultTextStyle(
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 14,
                      fontWeight: FontWeight.w800,
                    ),
                    child: widget.child,
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );

    if (!widget.animateEntrance) return content;
    return content
        .animate()
        .fadeIn(duration: 260.ms, curve: Curves.easeOutCubic)
        .slideY(begin: 0.08, end: 0, duration: 360.ms);
  }
}
