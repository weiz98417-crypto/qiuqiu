import 'package:flutter/material.dart';
import 'screens/match_screen.dart';

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
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: const Color(0xFFFF6B35),
          brightness: Brightness.dark,
        ),
        scaffoldBackgroundColor: const Color(0xFF1A1A2E),
      ),
      home: const MatchScreen(),
    );
  }
}
