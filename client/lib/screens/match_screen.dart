import 'package:flutter/material.dart';
import '../widgets/live2d_view.dart';
import '../services/websocket_service.dart';
import '../services/audio_player.dart';
import '../services/preferences_service.dart';
import '../services/vad_service.dart';
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
  final VADService _vad = VADService();
  UserProfile _profile = const UserProfile(nickname: '', favoriteTeam: '', talkativeness: 'normal');
  String _expression = 'idle';
  bool _isSpeaking = false;
  String _scoreText = '等待比赛...';
  bool _isRecording = false;
  bool _isVADMode = false; // false=pushToTalk, true=freeTalk

  @override
  void initState() {
    super.initState();
    _loadProfile();
    _ws.connect('ws://10.0.2.2:8080/ws/match/test?token=qiuqiu-dev-token');
    _ws.onMessage.listen(_handleMessage);
    _audio.stateStream.listen((state) {
      setState(() => _isSpeaking = state.isPlaying);
      // Interrupt: if user speaks while qiuqiu is speaking
      if (_isRecording && _isSpeaking) {
        _ws.send({'type': 'interrupt'});
        _audio.pause();
        setState(() => _expression = 'listening');
      }
    });
    _vad.events.listen((e) {
      if (e.state == VADState.sentenceEnd) {
        _ws.send({'type': 'user_speech', 'text': '', 'mode': 'voice'});
      }
      if (e.state == VADState.speaking && _isSpeaking) {
        _ws.send({'type': 'interrupt'});
        _audio.pause();
        setState(() => _expression = 'listening');
      }
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
        setState(() => _expression = msg['expression'] ?? 'idle');
        _audio.start();
      case 'expression':
        setState(() => _expression = msg['state'] ?? 'idle');
      case 'interrupt':
        _audio.pause();
        setState(() => _expression = 'listening');
      case 'match_status':
        setState(() => _scoreText = msg['status'] ?? '等待比赛...');
    }
  }

  void _onMicPress() {
    if (_isVADMode) {
      // Free talk: toggle
      if (_isRecording) {
        _vad.stopListening();
        setState(() => _isRecording = false);
      } else {
        _vad.startListening(VADMode.freeTalk);
        setState(() { _isRecording = true; _expression = 'listening'; });
      }
    } else {
      // Push to talk: hold
      _vad.startListening(VADMode.pushToTalk);
      _vad.onSpeechDetected();
      setState(() { _isRecording = true; _expression = 'listening'; });
    }
  }

  void _onMicRelease() {
    if (!_isVADMode) {
      _vad.onManualStop();
      setState(() => _isRecording = false);
    }
  }

  void _toggleVADMode() {
    setState(() => _isVADMode = !_isVADMode);
    if (_isRecording) {
      _vad.stopListening();
      _isRecording = false;
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
    if (saved != null) setState(() => _profile = saved);
  }

  @override
  void dispose() {
    _ws.disconnect();
    _audio.dispose();
    _vad.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final size = MediaQuery.of(context).size;
    return Scaffold(
      backgroundColor: const Color(0xFF1A1A2E),
      body: Column(
        children: [
          Container(
            padding: const EdgeInsets.symmetric(vertical: 10, horizontal: 16),
            color: Colors.black26,
            child: Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text(_scoreText, style: const TextStyle(fontSize: 14, color: Colors.white70)),
                Row(children: [
                  GestureDetector(
                    onTap: _toggleVADMode,
                    child: Text(_isVADMode ? '自由' : '按住', style: TextStyle(fontSize: 11, color: _isVADMode ? Colors.orange : Colors.white38)),
                  ),
                  IconButton(
                    icon: const Icon(Icons.settings, color: Colors.white54, size: 20),
                    onPressed: _openSettings,
                  ),
                ]),
              ],
            ),
          ),
          SizedBox(
            height: size.height * 0.62,
            child: Live2dView(expression: _expression, isSpeaking: _isSpeaking),
          ),
          Container(
            padding: const EdgeInsets.symmetric(vertical: 10),
            color: Colors.black26,
            child: Column(children: [
              GestureDetector(
                onTapDown: (_) => _onMicPress(),
                onTapUp: (_) => _onMicRelease(),
                onTapCancel: () => _onMicRelease(),
                child: Container(
                  width: 64, height: 64,
                  decoration: BoxDecoration(
                    shape: BoxShape.circle,
                    color: _isRecording ? Colors.red : Colors.white12,
                    border: Border.all(color: _isRecording ? Colors.redAccent : Colors.white24, width: 3),
                  ),
                  child: Icon(Icons.mic, color: _isRecording ? Colors.white : Colors.white54, size: 32),
                ),
              ),
              const SizedBox(height: 6),
              Text(_isRecording ? '收音中...' : _isVADMode ? '点击开始自由对话' : '按住说话',
                   style: const TextStyle(color: Colors.white38, fontSize: 11)),
            ]),
          ),
        ],
      ),
    );
  }
}
