import 'package:flutter/material.dart';
import 'screens/match_screen.dart';
import 'theme/app_theme.dart';

void main() {
  runApp(const QiuQiuApp());
}

class QiuQiuApp extends StatelessWidget {
  const QiuQiuApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: '球球',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.dark,
      themeMode: ThemeMode.dark,
      home: const MatchScreen(),
    );
  }
}
