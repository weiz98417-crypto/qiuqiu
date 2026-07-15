import 'package:flutter/material.dart';

abstract final class AppColors {
  static const night = Color(0xFF070B12);
  static const terrace = Color(0xFF111824);
  static const stage = Color(0xFF20262E);
  static const line = Color(0xFF253142);
  static const ink = Color(0xFFF4F1E8);
  static const muted = Color(0xFFAAB4C0);
  static const orange = Color(0xFFFF6B35);
  static const orangeDeep = Color(0xFFE83A24);
  static const green = Color(0xFF5FCB8B);
  static const yellow = Color(0xFFF2C94C);
  static const red = Color(0xFFF05D5E);
  static const paper = Color(0xFFF2EEE5);
  static const paperInk = Color(0xFF141414);
}

abstract final class AppSpacing {
  static const xxs = 4.0;
  static const xs = 8.0;
  static const sm = 12.0;
  static const md = 16.0;
  static const lg = 24.0;
  static const xl = 32.0;
  static const xxl = 48.0;
}

abstract final class AppFonts {
  static const body = 'Noto Sans SC';
  static const scoreboard = 'Barlow Condensed';
}

abstract final class AppTheme {
  static ThemeData get dark {
    const colorScheme = ColorScheme.dark(
      primary: AppColors.orange,
      onPrimary: Colors.white,
      secondary: AppColors.green,
      onSecondary: AppColors.night,
      surface: AppColors.terrace,
      onSurface: AppColors.ink,
      error: AppColors.red,
      outline: AppColors.line,
    );

    final baseTextTheme = ThemeData.dark().textTheme.apply(
      bodyColor: AppColors.ink,
      displayColor: AppColors.ink,
      fontFamily: AppFonts.body,
      fontFamilyFallback: const [
        'Noto Sans SC',
        'PingFang SC',
        'Microsoft YaHei',
      ],
    );

    return ThemeData(
      useMaterial3: true,
      brightness: Brightness.dark,
      colorScheme: colorScheme,
      scaffoldBackgroundColor: AppColors.night,
      fontFamily: AppFonts.body,
      textTheme: baseTextTheme.copyWith(
        displaySmall: baseTextTheme.displaySmall?.copyWith(
          fontSize: 40,
          height: 1.08,
          fontWeight: FontWeight.w900,
          letterSpacing: -1.2,
        ),
        headlineMedium: baseTextTheme.headlineMedium?.copyWith(
          fontSize: 28,
          height: 1.18,
          fontWeight: FontWeight.w800,
          letterSpacing: -0.5,
        ),
        titleLarge: baseTextTheme.titleLarge?.copyWith(
          fontSize: 20,
          height: 1.3,
          fontWeight: FontWeight.w700,
        ),
        bodyLarge: baseTextTheme.bodyLarge?.copyWith(
          fontSize: 16,
          height: 1.6,
          fontWeight: FontWeight.w400,
        ),
        bodyMedium: baseTextTheme.bodyMedium?.copyWith(
          fontSize: 14,
          height: 1.55,
          fontWeight: FontWeight.w400,
        ),
        labelLarge: baseTextTheme.labelLarge?.copyWith(
          fontSize: 14,
          fontWeight: FontWeight.w700,
        ),
        labelMedium: baseTextTheme.labelMedium?.copyWith(
          fontSize: 12,
          fontWeight: FontWeight.w600,
        ),
      ),
      appBarTheme: const AppBarTheme(
        backgroundColor: AppColors.night,
        foregroundColor: AppColors.ink,
        centerTitle: false,
        elevation: 0,
        scrolledUnderElevation: 0,
      ),
      inputDecorationTheme: const InputDecorationTheme(
        filled: true,
        fillColor: AppColors.terrace,
        border: OutlineInputBorder(
          borderRadius: BorderRadius.all(Radius.circular(8)),
          borderSide: BorderSide(color: AppColors.line),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.all(Radius.circular(8)),
          borderSide: BorderSide(color: AppColors.line),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.all(Radius.circular(8)),
          borderSide: BorderSide(color: AppColors.orange, width: 2),
        ),
      ),
      dividerTheme: const DividerThemeData(color: AppColors.line, thickness: 1),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size(48, 52),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
          textStyle: const TextStyle(fontSize: 14, fontWeight: FontWeight.w800),
        ),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
      ),
    );
  }
}
