import 'package:flutter/material.dart';
import '../widgets/live2d_view.dart';
import '../services/websocket_service.dart';
import '../services/audio_player.dart';
import '../services/preferences_service.dart';
import 'settings_screen.dart';

class MatchScreen extends StatefulWidget {
  const MatchScreen({super.key});

  @override
  State<MatchScreen> createState() => _MatchScreenState();
}

class _MatchScreenState extends State<MatchScreen> {
  final WebSocketService _ws = WebSocketService();
  final AudioPlayerService _audio = AudioPlayerService();
  final PreferencesService _prefs = PreferencesService();
  UserProfile _profile = const UserProfile(nickname: '', favoriteTeam: '', talkativeness: 'normal');
  String _expression = 'idle';
  bool _isSpeaking = false;
  String _scoreText = '等待比赛...';

  @override
  void initState() {
    super.initState();
    _loadProfile();
    _ws.connect('ws://10.0.2.2:8080/ws/match/test?token=qiuqiu-dev-token');
    _ws.onMessage.listen(_handleMessage);
    _audio.stateStream.listen((state) {
      setState(() => _isSpeaking = state.isPlaying);
    });
  }

  Future<void> _loadProfile() async {
    final p = await _prefs.load();
    setState(() => _profile = p);
  }

  void _handleMessage(Map<String, dynamic> msg) {
    switch (msg['type']) {
      case 'event':
        final data = msg['data'] as Map<String, dynamic>?;
        setState(() {
          _expression = msg['event'] == 'goal' ? 'excited' : 'idle';
          if (data != null && data['score'] != null) {
            _scoreText = '${data['score']}  ${data['minute'] ?? '0'}\'';
          }
        });
      case 'audio':
        setState(() {
          _expression = msg['expression'] ?? 'idle';
        });
        if (msg['data'] is List<int>) {
          _audio.start();
        }
      case 'expression':
        setState(() => _expression = msg['state'] ?? 'idle');
      case 'match_status':
        setState(() => _scoreText = msg['status'] ?? '等待比赛...');
    }
  }

  void _openSettings() async {
    final saved = await Navigator.push<UserProfile>(
      context,
      MaterialPageRoute(
        builder: (_) => SettingsScreen(
          initialProfile: _profile,
          onSave: (p) => _prefs.save(p),
        ),
      ),
    );
    if (saved != null) {
      setState(() => _profile = saved);
    }
  }

  @override
  void dispose() {
    _ws.disconnect();
    _audio.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final size = MediaQuery.of(context).size;
    return Scaffold(
      backgroundColor: const Color(0xFF1A1A2E),
      body: Column(
        children: [
          // Score bar
          Container(
            padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 16),
            color: Colors.black26,
            child: Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text(_scoreText, style: const TextStyle(fontSize: 14, color: Colors.white70)),
                IconButton(
                  icon: const Icon(Icons.settings, color: Colors.white54, size: 20),
                  onPressed: _openSettings,
                ),
              ],
            ),
          ),
          // Live2D
          SizedBox(
            height: size.height * 0.65,
            child: Live2dView(expression: _expression, isSpeaking: _isSpeaking),
          ),
          // Control
          Container(
            padding: const EdgeInsets.all(16),
            color: Colors.black26,
            child: const Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(Icons.sports_soccer, color: Colors.white54, size: 24),
                SizedBox(width: 12),
                Text('球球陪你看球', style: TextStyle(color: Colors.white54, fontSize: 14)),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
