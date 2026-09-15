import 'package:flutter/material.dart';

import '../theme/app_theme.dart';

class MobileThemeCanvas extends StatelessWidget {
  final String backgroundAsset;
  final Color overlayColor;
  final Widget child;

  const MobileThemeCanvas({
    super.key,
    required this.backgroundAsset,
    required this.overlayColor,
    required this.child,
  });

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: AppColors.night,
      child: LayoutBuilder(
        builder: (context, constraints) {
          const maximumPhoneWidth = 520.0;
          final fillsViewport = constraints.maxWidth <= maximumPhoneWidth;
          final canvasWidth =
              fillsViewport ? constraints.maxWidth : maximumPhoneWidth;
          return Align(
            alignment: Alignment.topCenter,
            child: SizedBox(
              width: canvasWidth,
              height: constraints.maxHeight,
              child: ClipRRect(
                borderRadius: BorderRadius.circular(fillsViewport ? 0 : 24),
                child: Stack(
                  fit: StackFit.expand,
                  children: [
                    Image.asset(
                      backgroundAsset,
                      fit: BoxFit.cover,
                      errorBuilder: (_, __, ___) => const ColoredBox(
                        color: AppColors.terrace,
                      ),
                    ),
                    ColoredBox(color: overlayColor),
                    child,
                  ],
                ),
              ),
            ),
          );
        },
      ),
    );
  }
}
