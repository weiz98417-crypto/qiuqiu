import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import '../widgets/live2d_view.dart';
import '../services/websocket_service.dart';
import '../services/audio_player.dart';
import '../services/preferences_service.dart';
import '../services/recorder_stub.dart';
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
  String? _motion;
  String _scoreText = '等待比赛...';
  String _debugVAD = '';
  bool _isRecording = false;
  bool _isVADMode = false;

  @override
  void initState() {
    super.initState();
    _loadProfile();
    final host = kIsWeb ? 'localhost' : '10.0.2.2';
    _ws.connect('ws://$host:8080/ws/match/test?token=qiuqiu-dev-token');
    _ws.onMessage.listen(_handleMessage);
    _audio.stateStream.listen((state) {
      setState(() => _isSpeaking = state.isPlaying);
      if (_isRecording && _isSpeaking) {
        _ws.send({'type': 'interrupt'});
        _audio.pause();
        setState(() => _expression = 'listening');
      }
    });
    _ws.onBinary.listen((Uint8List pcm) {
      _audio.pushPcmFrame(pcm);
    });
    _vad.events.listen((e) {
      setState(() => _debugVAD = 'VAD: ${e.state.name}');
      if (e.state == VADState.sentenceEnd) {
        final audio = _vad.drainAudio();
        _ws.send({
          'type': 'user_speech', 'text': '', 'mode': 'voice',
          'audio': audio != null ? base64Encode(audio) : '',
          'talkativeness': _profile.talkativeness,
        });
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

  String _expressionToMotion(String expr) {
    const map = {'idle':'idle','listening':'listen','excited':'cheer','happy':'cheer',
      'surprised':'think','nervous':'listen','confused':'think','tease':'idle','chat':'speak'};
    return map[expr] ?? 'idle';
  }

  String? _eventToMotion(String event) {
    const map = {'goal':'cheer','match_start':'hello','penalty':'cheer',
      'red_card':'think','yellow_card':'think','match_end':'idle'};
    return map[event];
  }

  void _handleMessage(Map<String, dynamic> msg) {
    switch (msg['type']) {
      case 'event':
        final eventType = msg['event'] as String? ?? '';
        final data = msg['data'] as Map<String, dynamic>?;
        setState(() {
          _expression = eventType == 'goal' ? 'excited' : 'idle';
          _motion = _eventToMotion(eventType);
          if (data != null && data['score'] != null) {
            _scoreText = '${data['score']}  ${data['minute'] ?? '0'}\'';
          }
        });
      case 'audio':
        setState(() => _expression = msg['expression'] ?? 'idle');
        _audio.start();
      case 'expression':
        final state = msg['state'] as String? ?? 'idle';
        setState(() { _expression = state; _motion = _expressionToMotion(state); });
      case 'interrupt':
        _audio.pause();
        setState(() { _expression = 'listening'; _motion = 'listen'; });
      case 'match_status':
        setState(() => _scoreText = msg['status'] ?? '等待比赛...');
    }
  }

  void _onMicPress() {
    if (_isVADMode) {
      if (_isRecording) {
        _vad.stopListening();
        setState(() => _isRecording = false);
      } else {
        _vad.startListening(VADMode.freeTalk);
        setState(() { _isRecording = true; _expression = 'listening'; });
      }
    } else {
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
            child: Live2dView(expression: _expression, isSpeaking: _isSpeaking, motion: _motion),
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
              if (_debugVAD.isNotEmpty)
                Text(_debugVAD, style: const TextStyle(color: Colors.orange, fontSize: 10)),
              Text(_vad.debugInfo, style: const TextStyle(color: Colors.green, fontSize: 9)),
            ]),
          ),
        ],
      ),
    );
  }
}
