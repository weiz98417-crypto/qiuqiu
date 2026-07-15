import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/foundation.dart' show debugPrint, kIsWeb, listEquals;
import 'package:flutter/material.dart';

import '../services/audio_player.dart';
import '../services/preferences_service.dart';
import '../services/recorder_stub.dart';
import '../services/websocket_service.dart';
import '../theme/app_theme.dart';
import '../widgets/live2d_view.dart';
import 'reply_display.dart';
import 'settings_screen.dart';

enum ConversationPhase {
  idle,
  welcoming,
  listening,
  userSpeaking,
  understanding,
  speaking,
  permissionDenied,
  offline,
  failed,
}

class MatchScreen extends StatefulWidget {
  const MatchScreen({super.key});

  @override
  State<MatchScreen> createState() => _MatchScreenState();
}

class _MatchScreenState extends State<MatchScreen> {
  static const _configuredSocketUrl = String.fromEnvironment('QIUQIU_WS_URL');
  static const _configuredToken = String.fromEnvironment('QIUQIU_APP_TOKEN');

  final WebSocketService _socket = WebSocketService();
  final AudioPlayerService _audio = AudioPlayerService();
  final PreferencesService _preferences = PreferencesService();
  final VADService _vad = VADService();
  final TextEditingController _textController = TextEditingController();
  final GlobalKey<Live2dViewState> _live2dKey = GlobalKey<Live2dViewState>();
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  late final Future<void> _profileLoad;

  UserProfile _profile = const UserProfile(
    nickname: '',
    favoriteTeam: '利物浦',
    talkativeness: 'normal',
  );
  MatchViewData _match = const MatchViewData();
  SocketStatus _socketStatus = SocketStatus.connecting;
  ConversationPhase _phase = ConversationPhase.idle;
  final PendingAudioQueue _pendingAudio = PendingAudioQueue();
  bool _insideMatch = true;
  bool _textMode = false;
  bool _isHoldingToTalk = false;
  bool _firstMeetingCompleted = false;
  bool _awaitingFirstMeetingGreeting = false;
  String _userId = '';
  int _signalSequence = 0;
  String _expression = 'idle';
  String? _motion;
  CompanionPresentation? _activePresentation;
  Timer? _presentationReturnTimer;
  String _qiuqiuLine = '今晚我在。开场以后，想说什么直接说。';
  String _qiuqiuDetail = '我会跟着比赛节奏回应，不打断你看球。';
  String _userLine = '';
  String? _notice;

  bool get _continuousEnabled => _profile.continuousConversation;
  bool get _isSpeaking => _phase == ConversationPhase.speaking;

  String _nextSignalId() {
    _signalSequence += 1;
    return 'turn_${_userId}_${DateTime.now().microsecondsSinceEpoch}_$_signalSequence';
  }

  @override
  void initState() {
    super.initState();
    _bindServices();
    _profileLoad = _loadProfile();
    unawaited(_profileLoad);
    _socket.connect(_socketUrl(), token: _configuredToken);
  }

  void _bindServices() {
    _subscriptions.addAll([
      _socket.onMessage.listen(_handleSocketMessage),
      _socket.onBinary.listen(_handleAudioBytes),
      _socket.statusStream.listen(_handleSocketStatus),
      _vad.events.listen(_handleVadEvent),
      _audio.stateStream.listen(_handleAudioState),
    ]);
  }

  String _socketUrl() {
    if (_configuredSocketUrl.isNotEmpty) return _configuredSocketUrl;
    if (kIsWeb) {
      final page = Uri.base;
      return page
          .replace(
            scheme: page.scheme == 'https' ? 'wss' : 'ws',
            path: '/ws/match/test',
            query: null,
            fragment: null,
          )
          .toString();
    }
    return 'ws://10.0.2.2:8080/ws/match/test';
  }

  Future<void> _loadProfile() async {
    final profile = await _preferences.load();
    final userId = await _preferences.loadOrCreateAnonymousUserId();
    final firstMeetingCompleted = await _preferences.hasCompletedFirstMeeting();
    await _audio.setMuted(!profile.soundEnabled);
    if (!mounted) return;
    setState(() {
      _profile = profile;
      _userId = userId;
      _firstMeetingCompleted = firstMeetingCompleted;
    });
  }

  void _handleSocketStatus(SocketStatus status) {
    if (!mounted) return;
    setState(() {
      _socketStatus = status;
      if (status == SocketStatus.connected) {
        if (_phase == ConversationPhase.offline) {
          _phase = _continuousEnabled
              ? ConversationPhase.listening
              : ConversationPhase.idle;
        }
        _notice = null;
      } else if (status == SocketStatus.failed) {
        _phase = ConversationPhase.offline;
        _notice = '暂时没连上比赛，但你仍可以留在这里。';
      }
    });
    if (status == SocketStatus.connected) {
      unawaited(_enterMatchAfterProfile());
    }
  }

  Future<void> _enterMatchAfterProfile() async {
    await _profileLoad;
    if (!mounted || _socketStatus != SocketStatus.connected) return;
    await _enterMatch();
  }

  void _handleSocketMessage(Map<String, dynamic> message) {
    if (!mounted) return;
    final type = message['type'] as String? ?? '';
    switch (type) {
      case 'match_snapshot':
        final snapshot = _map(message['data']);
        if (snapshot != null) {
          setState(() => _match = _match.withSnapshot(snapshot));
        }
        break;
      case 'match_event':
        final event = _map(message['data']);
        final snapshot = _map(message['snapshot']);
        setState(() {
          if (snapshot != null) _match = _match.withSnapshot(snapshot);
          if (event != null) {
            _match = _match.withEvent(event);
          }
        });
        break;
      case 'presentation':
        final presentation = CompanionPresentation.fromReplyData({
          'presentation': _map(message['data']),
        });
        if (presentation != null) {
          _applyPresentation(presentation);
        }
        break;
      case 'event':
        _handleLegacyEvent(message);
        break;
      case 'expression':
        final expression = message['state'] as String? ?? 'idle';
        setState(() {
          _clearPresentationFields();
          _expression = expression;
          _motion = _motionForExpression(expression);
        });
        break;
      case 'voice_audio':
        _pendingAudio.add(PendingAudio(
          mime: message['mime'] as String? ?? 'audio/wav',
          traceId: message['traceId'] as String?,
          byteLength: _integer(message['byteLength']),
        ));
        break;
      case 'voice_status':
        _handleVoiceStatus(message);
        break;
      case 'interrupt':
        _pendingAudio.clear();
        unawaited(_audio.pause(notify: false));
        setState(() {
          _clearPresentationFields();
          _phase = ConversationPhase.listening;
          _expression = 'listening';
          _motion = 'listen';
        });
        break;
      case 'reconnecting':
        setState(() => _notice = '正在回到比赛现场…');
        break;
    }
  }

  void _handleLegacyEvent(Map<String, dynamic> message) {
    final eventType = message['event'] as String? ?? '';
    final data = _map(message['data']);
    if (eventType == 'qiuqiu_reply') {
      final reply = data?['text'] as String?;
      if (reply == null || reply.trim().isEmpty) return;
      final traceId = data?['traceId'] as String?;
      final isFirstMeeting = data?['source'] == 'first_meeting';
      final presentation = CompanionPresentation.fromReplyData(data);
      final parts = splitReplyForDisplay(reply.trim());
      setState(() {
        _qiuqiuLine = parts.$1;
        _qiuqiuDetail = parts.$2;
        _phase = isFirstMeeting
            ? ConversationPhase.welcoming
            : ConversationPhase.understanding;
        if (presentation != null) {
          _activatePresentationFields(
            presentation,
          );
        } else {
          _expression = isFirstMeeting ? 'happy' : 'chat';
          _motion = isFirstMeeting ? 'hello' : 'speak';
        }
        if (isFirstMeeting) {
          _firstMeetingCompleted = true;
        }
      });
      if (isFirstMeeting) {
        unawaited(_preferences.markFirstMeetingCompleted());
      }
      if (traceId != null && traceId.trim().isNotEmpty) {
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (!mounted) return;
          _socket.send({'type': 'reply_displayed', 'traceId': traceId});
        });
      }
      return;
    }

    setState(() {
      _motion = _motionForEvent(eventType);
      _expression = _expressionForEvent(eventType);
      final score = data?['score'];
      if (score is String) _match = _match.withLegacyScore(score);
      final minute = data?['minute'];
      if (minute != null) _match = _match.copyWith(clock: "$minute'");
    });
  }

  void _handleVoiceStatus(Map<String, dynamic> message) {
    final state = message['state'] as String? ?? '';
    setState(() {
      if (state == 'text_fallback') {
        _notice = '这句没听清，已经用文字继续。';
      } else if (state == 'tts_fallback') {
        _notice = '声音暂时没出来，回答已显示在字幕里。';
        _phase = _continuousEnabled
            ? ConversationPhase.listening
            : ConversationPhase.idle;
        _finishFirstMeetingGreeting();
        _schedulePresentationReturn();
      } else if (state == 'failed') {
        _notice = '这句没听清，再说一次就好。';
        _phase = ConversationPhase.failed;
        _finishFirstMeetingGreeting();
        _schedulePresentationReturn();
      }
    });
  }

  void _handleAudioBytes(Uint8List audioBytes) {
    final metadata =
        _pendingAudio.take() ?? const PendingAudio(mime: 'audio/wav');
    if (!_profile.soundEnabled) {
      final receipt = mutedPlaybackReceipt(metadata);
      if (receipt != null) {
        _socket.send(receipt);
      }
      setState(() {
        _phase = _continuousEnabled
            ? ConversationPhase.listening
            : ConversationPhase.idle;
      });
      _schedulePresentationReturn();
      _finishFirstMeetingGreeting();
      return;
    }
    unawaited(
      _audio.playEncoded(
        audioBytes,
        mime: metadata.mime,
        traceId: metadata.traceId,
        speed: presentationPlaybackSpeed(_activePresentation),
      ),
    );
  }

  void _handleVadEvent(VADEvent event) {
    if (!mounted) return;
    switch (event.state) {
      case VADState.listening:
        _socket.send({'type': 'user_activity', 'state': 'idle'});
        setState(() {
          _phase = ConversationPhase.listening;
          if (_activePresentation == null) {
            _expression = 'listening';
            _motion = 'listen';
          }
          _notice = null;
        });
        break;
      case VADState.speaking:
        _socket.send({'type': 'user_activity', 'state': 'speaking'});
        if (_isSpeaking) {
          _socket.send({'type': 'interrupt'});
          unawaited(_audio.pause());
        }
        setState(() {
          _clearPresentationFields();
          _phase = ConversationPhase.userSpeaking;
          _expression = 'focus';
          _motion = 'focus';
        });
        break;
      case VADState.sentenceEnd:
        final audio = _vad.drainAudio();
        if (audio == null || audio.isEmpty) return;
        final sent = _socket.send({
          'type': 'user_speech',
          'userId': _userId,
          'signalId': _nextSignalId(),
          'text': '',
          'mode': 'voice',
          'audio': base64Encode(audio),
          'talkativeness': _profile.talkativeness,
        });
        setState(() {
          _phase = sent
              ? ConversationPhase.understanding
              : ConversationPhase.offline;
          _userLine = '刚刚说的话';
          if (sent) {
            _expression = 'thinking';
            _motion = 'think';
          }
          if (!sent) _notice = '现在还没连上，稍后再试一次。';
        });
        break;
      case VADState.idle:
        _socket.send({'type': 'user_activity', 'state': 'idle'});
        setState(() {
          if (_phase != ConversationPhase.offline) {
            _phase = ConversationPhase.idle;
          }
        });
        break;
      case VADState.permissionDenied:
        setState(() {
          _phase = ConversationPhase.permissionDenied;
          _textMode = true;
          _notice = '没有麦克风权限，先打字也能继续陪看。';
        });
        break;
      case VADState.failure:
        setState(() {
          _phase = ConversationPhase.failed;
          _notice = '麦克风暂时没准备好，可以重试或打字。';
        });
        break;
    }
  }

  void _handleAudioState(AudioState state) {
    if (!mounted) return;
    final traceId = state.traceId;
    switch (state.status) {
      case AudioPlaybackStatus.started:
        if (traceId != null) {
          _socket.send({
            'type': 'voice_playback',
            'traceId': traceId,
            'state': 'started',
          });
        }
        setState(() {
          _phase = ConversationPhase.speaking;
          if (_activePresentation == null) {
            _expression = 'chat';
            _motion = 'speak';
          }
          _notice = null;
        });
        _presentationReturnTimer?.cancel();
        break;
      case AudioPlaybackStatus.ended:
        if (traceId != null) {
          _socket.send({
            'type': 'voice_playback',
            'traceId': traceId,
            'state': 'ended',
          });
        }
        setState(() {
          _phase = _continuousEnabled
              ? ConversationPhase.listening
              : ConversationPhase.idle;
          if (_activePresentation == null) {
            _expression = _continuousEnabled ? 'listening' : 'idle';
            _motion = _continuousEnabled ? 'listen' : 'idle';
          }
          _notice = null;
        });
        _schedulePresentationReturn();
        _finishFirstMeetingGreeting();
        break;
      case AudioPlaybackStatus.interrupted:
        if (traceId != null) {
          _socket.send({
            'type': 'voice_playback',
            'traceId': traceId,
            'state': 'interrupted',
          });
        }
        setState(() {
          _clearPresentationFields();
          _phase = ConversationPhase.listening;
          _expression = 'listening';
          _motion = 'listen';
        });
        break;
      case AudioPlaybackStatus.blocked:
        if (traceId != null) {
          _socket.send({
            'type': 'voice_playback',
            'traceId': traceId,
            'state': 'blocked',
          });
        }
        setState(() {
          _notice = '轻触一下屏幕，我就能开口。';
          _phase = _continuousEnabled
              ? ConversationPhase.listening
              : ConversationPhase.idle;
        });
        _schedulePresentationReturn();
        _finishFirstMeetingGreeting();
        break;
      case AudioPlaybackStatus.failed:
        debugPrint('Audio playback failed: ${state.error}');
        if (traceId != null) {
          _socket.send({
            'type': 'voice_playback',
            'traceId': traceId,
            'state': 'error',
          });
        }
        setState(() {
          _notice = '声音暂时没播放出来，字幕还在。';
          _phase = _continuousEnabled
              ? ConversationPhase.listening
              : ConversationPhase.idle;
        });
        _schedulePresentationReturn();
        break;
    }
  }

  Future<void> _enterMatch() async {
    await _profileLoad;
    if (!mounted) return;
    setState(() => _insideMatch = true);
    _socket.send({'type': 'identify', 'userId': _userId});
    _socket.send({'type': 'session_opened', 'userId': _userId});
    if (!_firstMeetingCompleted) {
      final sent = _socket.send({
        'type': 'first_meeting',
        'userId': _userId,
        'nickname': _profile.nickname,
        'favoriteTeam': _profile.favoriteTeam,
      });
      if (sent) {
        setState(() {
          _awaitingFirstMeetingGreeting = true;
          _phase = ConversationPhase.welcoming;
          _expression = 'happy';
          _motion = 'hello';
          _qiuqiuLine = '嗨，我是球球。';
          _qiuqiuDetail = '第一次见面，先让我认真和你打个招呼。';
          _notice = null;
        });
        return;
      }
    }
    if (_continuousEnabled) {
      await _vad.startListening(VADMode.freeTalk);
    }
  }

  void _finishFirstMeetingGreeting() {
    if (!_awaitingFirstMeetingGreeting) return;
    _awaitingFirstMeetingGreeting = false;
    if (_insideMatch && _continuousEnabled) {
      unawaited(_vad.startListening(VADMode.freeTalk));
    }
  }

  Future<void> _toggleContinuous() async {
    final enabled = !_continuousEnabled;
    final updated = _profile.copyWith(continuousConversation: enabled);
    setState(() {
      _profile = updated;
      _notice = enabled ? null : '连续对话已关闭，按住麦克风仍能说话。';
    });
    await _preferences.save(updated);
    if (enabled && !_awaitingFirstMeetingGreeting) {
      await _vad.startListening(VADMode.freeTalk);
    } else {
      _vad.stopListening();
      await _audio.pause();
    }
  }

  Future<void> _startPushToTalk() async {
    if (_continuousEnabled || _isHoldingToTalk) return;
    _isHoldingToTalk = true;
    await _vad.startListening(VADMode.pushToTalk);
    _vad.onSpeechDetected();
  }

  void _stopPushToTalk() {
    if (!_isHoldingToTalk) return;
    _isHoldingToTalk = false;
    _vad.onManualStop();
  }

  void _sendText() {
    final text = _textController.text.trim();
    if (text.isEmpty) return;
    final sent = _socket.send({
      'type': 'user_speech',
      'userId': _userId,
      'signalId': _nextSignalId(),
      'text': text,
      'mode': 'text',
      'audio': '',
      'talkativeness': _profile.talkativeness,
    });
    setState(() {
      _userLine = text;
      _phase =
          sent ? ConversationPhase.understanding : ConversationPhase.offline;
      if (sent) {
        _clearPresentationFields();
        _expression = 'thinking';
        _motion = 'think';
        _textController.clear();
      } else {
        _notice = '现在还没连上，文字没有发出去。';
      }
    });
  }

  Future<void> _openSettings() async {
    final saved = await Navigator.push<UserProfile>(
      context,
      MaterialPageRoute(
        builder: (_) =>
            SettingsScreen(initialProfile: _profile, onSave: _preferences.save),
      ),
    );
    if (saved == null || !mounted) return;
    final continuousChanged =
        saved.continuousConversation != _profile.continuousConversation;
    setState(() => _profile = saved);
    await _audio.setMuted(!saved.soundEnabled);
    if (_insideMatch && continuousChanged) {
      if (saved.continuousConversation && !_awaitingFirstMeetingGreeting) {
        await _vad.startListening(VADMode.freeTalk);
      } else {
        _vad.stopListening();
      }
    }
  }

  String _motionForExpression(String expression) {
    const motions = {
      'idle': 'idle',
      'listening': 'listen',
      'focus': 'focus',
      'thinking': 'think',
      'excited': 'cheer',
      'happy': 'cheer',
      'surprised': 'think',
      'nervous': 'listen',
      'confused': 'think',
      'tease': 'idle',
      'chat': 'speak',
    };
    return motions[expression] ?? 'idle';
  }

  void _applyPresentation(
    CompanionPresentation presentation, {
    bool awaitingPlayback = false,
  }) {
    if (!mounted) return;
    setState(() {
      _activatePresentationFields(
        presentation,
      );
    });
    if (!awaitingPlayback) {
      _schedulePresentationReturn();
    }
  }

  void _activatePresentationFields(
    CompanionPresentation presentation,
  ) {
    _presentationReturnTimer?.cancel();
    _activePresentation = presentation;
    _expression = presentation.expression;
    _motion = presentation.motion;
  }

  void _schedulePresentationReturn() {
    final presentation = _activePresentation;
    if (presentation == null) return;
    _presentationReturnTimer?.cancel();
    _presentationReturnTimer = Timer(presentation.hold, () {
      if (!mounted || !identical(_activePresentation, presentation)) return;
      final resting = presentationReturnState(presentation);
      setState(() {
        _activePresentation = null;
        _expression = resting.$1;
        _motion = resting.$2;
      });
    });
  }

  void _clearPresentationFields() {
    _presentationReturnTimer?.cancel();
    _activePresentation = null;
  }

  String _motionForEvent(String event) {
    const motions = {
      'goal': 'cheer',
      'match_start': 'hello',
      'penalty': 'cheer',
      'red_card': 'think',
      'yellow_card': 'think',
      'match_end': 'idle',
    };
    return motions[event] ?? 'idle';
  }

  String _expressionForEvent(String event) {
    if (event == 'goal' || event == 'penalty') return 'excited';
    if (event == 'red_card' || event == 'yellow_card') return 'surprised';
    return 'idle';
  }

  @override
  void dispose() {
    _presentationReturnTimer?.cancel();
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    _textController.dispose();
    _vad.dispose();
    unawaited(_audio.dispose());
    unawaited(_socket.dispose());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: DecoratedBox(
        decoration: const BoxDecoration(color: AppColors.night),
        child: SafeArea(
          child: Align(
            alignment: Alignment.topCenter,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 520),
              child: _insideMatch
                  ? _LiveMatchExperience(
                      key: const ValueKey('live'),
                      live2dKey: _live2dKey,
                      match: _match,
                      expression: _expression,
                      motion: _motion,
                      isSpeaking: _isSpeaking,
                      phase: _phase,
                      socketStatus: _socketStatus,
                      continuousEnabled: _continuousEnabled,
                      subtitlesEnabled: _profile.subtitlesEnabled,
                      qiuqiuLine: _qiuqiuLine,
                      qiuqiuDetail: _qiuqiuDetail,
                      userLine: _userLine,
                      notice: _notice,
                      textMode: _textMode,
                      textController: _textController,
                      onToggleContinuous: _toggleContinuous,
                      onOpenSettings: _openSettings,
                      onOpenText: () => setState(() => _textMode = true),
                      onCloseText: () => setState(() => _textMode = false),
                      onSendText: _sendText,
                      onMicDown: _startPushToTalk,
                      onMicUp: _stopPushToTalk,
                    )
                  : _MatchLobby(
                      key: const ValueKey('lobby'),
                      live2dKey: _live2dKey,
                      match: _match,
                      expression: _expression,
                      motion: _motion,
                      socketStatus: _socketStatus,
                      onEnter: _enterMatch,
                      onOpenSettings: _openSettings,
                    ),
            ),
          ),
        ),
      ),
    );
  }
}

class _MatchLobby extends StatelessWidget {
  final GlobalKey<Live2dViewState> live2dKey;
  final MatchViewData match;
  final String expression;
  final String? motion;
  final SocketStatus socketStatus;
  final VoidCallback onEnter;
  final VoidCallback onOpenSettings;

  const _MatchLobby({
    super.key,
    required this.live2dKey,
    required this.match,
    required this.expression,
    required this.motion,
    required this.socketStatus,
    required this.onEnter,
    required this.onOpenSettings,
  });

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: AppColors.paper,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.md,
          AppSpacing.sm,
          AppSpacing.md,
          AppSpacing.md,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    '${match.competition} · ${match.liveLabel}',
                    style: Theme.of(context).textTheme.labelMedium?.copyWith(
                          color: AppColors.paperInk,
                          letterSpacing: 1,
                        ),
                  ),
                ),
                _ConnectionMark(status: socketStatus, dark: true),
                const SizedBox(width: AppSpacing.xs),
                IconButton(
                  tooltip: '陪看设置',
                  onPressed: onOpenSettings,
                  color: AppColors.paperInk,
                  icon: const Icon(Icons.tune_rounded),
                ),
              ],
            ),
            const SizedBox(height: AppSpacing.xs),
            Text(
              '今晚，\n别一个人看。',
              style: Theme.of(
                context,
              ).textTheme.displaySmall?.copyWith(color: AppColors.paperInk),
            ),
            const SizedBox(height: AppSpacing.lg),
            Expanded(
              child: DecoratedBox(
                decoration: BoxDecoration(
                  color: AppColors.stage,
                  border: Border.all(color: AppColors.paperInk, width: 2),
                ),
                child: Stack(
                  children: [
                    Positioned.fill(
                      child: Live2dView(
                        key: live2dKey,
                        expression: expression,
                        isSpeaking: false,
                        motion: motion,
                      ),
                    ),
                    Positioned(
                      left: AppSpacing.md,
                      top: AppSpacing.md,
                      child: RotatedBox(
                        quarterTurns: 1,
                        child: Text(
                          '${match.homeTeam.toUpperCase()} VS ${match.awayTeam.toUpperCase()}',
                          style:
                              Theme.of(context).textTheme.labelLarge?.copyWith(
                                    color: AppColors.ink,
                                    letterSpacing: 1,
                                  ),
                        ),
                      ),
                    ),
                    Positioned(
                      left: AppSpacing.md,
                      bottom: AppSpacing.md,
                      child: Text(
                        '${match.homeScore}—${match.awayScore}',
                        style:
                            Theme.of(context).textTheme.displaySmall?.copyWith(
                          color: AppColors.ink,
                          fontSize: 64,
                          height: 0.95,
                          fontFamily: AppFonts.scoreboard,
                          fontFeatures: const [
                            FontFeature.tabularFigures(),
                          ],
                        ),
                      ),
                    ),
                    Positioned(
                      right: AppSpacing.md,
                      bottom: AppSpacing.md,
                      child: Text(
                        '第 ${match.clock} 分钟',
                        style: Theme.of(
                          context,
                        ).textTheme.labelLarge?.copyWith(
                              color: AppColors.ink,
                              fontFamily: AppFonts.scoreboard,
                            ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: AppSpacing.md),
            FilledButton(
              onPressed: onEnter,
              style: FilledButton.styleFrom(
                backgroundColor: AppColors.orangeDeep,
                foregroundColor: Colors.white,
                side: const BorderSide(color: AppColors.paperInk, width: 2),
                shadowColor: AppColors.paperInk,
                elevation: 4,
              ),
              child: const Text('进入球球的看台'),
            ),
          ],
        ),
      ),
    );
  }
}

class _LiveMatchExperience extends StatelessWidget {
  final GlobalKey<Live2dViewState> live2dKey;
  final MatchViewData match;
  final String expression;
  final String? motion;
  final bool isSpeaking;
  final ConversationPhase phase;
  final SocketStatus socketStatus;
  final bool continuousEnabled;
  final bool subtitlesEnabled;
  final String qiuqiuLine;
  final String qiuqiuDetail;
  final String userLine;
  final String? notice;
  final bool textMode;
  final TextEditingController textController;
  final VoidCallback onToggleContinuous;
  final VoidCallback onOpenSettings;
  final VoidCallback onOpenText;
  final VoidCallback onCloseText;
  final VoidCallback onSendText;
  final Future<void> Function() onMicDown;
  final VoidCallback onMicUp;

  const _LiveMatchExperience({
    super.key,
    required this.live2dKey,
    required this.match,
    required this.expression,
    required this.motion,
    required this.isSpeaking,
    required this.phase,
    required this.socketStatus,
    required this.continuousEnabled,
    required this.subtitlesEnabled,
    required this.qiuqiuLine,
    required this.qiuqiuDetail,
    required this.userLine,
    required this.notice,
    required this.textMode,
    required this.textController,
    required this.onToggleContinuous,
    required this.onOpenSettings,
    required this.onOpenText,
    required this.onCloseText,
    required this.onSendText,
    required this.onMicDown,
    required this.onMicUp,
  });

  @override
  Widget build(BuildContext context) {
    final largeText = MediaQuery.textScalerOf(context).scale(16) > 22;
    return ColoredBox(
      color: AppColors.stage,
      child: Column(
        children: [
          if (largeText)
            _HorizontalScoreBar(match: match)
          else
            const SizedBox.shrink(),
          Expanded(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                if (!largeText)
                  _ScoreRail(match: match, onOpenSettings: onOpenSettings),
                Expanded(
                  child: Column(
                    children: [
                      Expanded(
                        child: _CharacterStage(
                          match: match,
                          live2dKey: live2dKey,
                          expression: expression,
                          motion: motion,
                          isSpeaking: isSpeaking,
                          socketStatus: socketStatus,
                          subtitlesEnabled: subtitlesEnabled,
                          qiuqiuLine: qiuqiuLine,
                          qiuqiuDetail: qiuqiuDetail,
                        ),
                      ),
                      _ConversationDock(
                        phase: phase,
                        continuousEnabled: continuousEnabled,
                        userLine: userLine,
                        notice: notice,
                        textMode: textMode,
                        textController: textController,
                        onToggleContinuous: onToggleContinuous,
                        onOpenSettings: onOpenSettings,
                        onOpenText: onOpenText,
                        onCloseText: onCloseText,
                        onSendText: onSendText,
                        onMicDown: onMicDown,
                        onMicUp: onMicUp,
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _ScoreRail extends StatelessWidget {
  final MatchViewData match;
  final VoidCallback onOpenSettings;

  const _ScoreRail({required this.match, required this.onOpenSettings});

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: 76,
      child: ColoredBox(
        color: AppColors.orangeDeep,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: AppSpacing.md),
          child: Column(
            children: [
              Text(
                '${match.homeScore}\n—\n${match.awayScore}',
                textAlign: TextAlign.center,
                style: Theme.of(context).textTheme.displaySmall?.copyWith(
                  color: Colors.white,
                  height: 0.95,
                  fontFamily: AppFonts.scoreboard,
                  fontFeatures: const [FontFeature.tabularFigures()],
                ),
              ),
              const SizedBox(height: AppSpacing.lg),
              Expanded(
                child: RotatedBox(
                  quarterTurns: 1,
                  child: Text(
                    '${match.homeTeam} · ${match.awayTeam} · ${match.clock}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.labelMedium?.copyWith(
                          color: Colors.white,
                          letterSpacing: 1,
                          fontFamily: AppFonts.scoreboard,
                        ),
                  ),
                ),
              ),
              IconButton(
                tooltip: '陪看设置',
                onPressed: onOpenSettings,
                color: Colors.white,
                icon: const Icon(Icons.tune_rounded),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _HorizontalScoreBar extends StatelessWidget {
  final MatchViewData match;

  const _HorizontalScoreBar({required this.match});

  @override
  Widget build(BuildContext context) {
    return ColoredBox(
      color: AppColors.orangeDeep,
      child: Padding(
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.md,
          vertical: AppSpacing.sm,
        ),
        child: Row(
          children: [
            Expanded(child: Text(match.homeTeam)),
            Text(
              '${match.homeScore} — ${match.awayScore}',
              style: Theme.of(context).textTheme.titleLarge?.copyWith(
                fontFamily: AppFonts.scoreboard,
                fontFeatures: const [FontFeature.tabularFigures()],
              ),
            ),
            Expanded(child: Text(match.awayTeam, textAlign: TextAlign.end)),
          ],
        ),
      ),
    );
  }
}

class _CharacterStage extends StatelessWidget {
  final GlobalKey<Live2dViewState> live2dKey;
  final MatchViewData match;
  final String expression;
  final String? motion;
  final bool isSpeaking;
  final SocketStatus socketStatus;
  final bool subtitlesEnabled;
  final String qiuqiuLine;
  final String qiuqiuDetail;

  const _CharacterStage({
    required this.match,
    required this.live2dKey,
    required this.expression,
    required this.motion,
    required this.isSpeaking,
    required this.socketStatus,
    required this.subtitlesEnabled,
    required this.qiuqiuLine,
    required this.qiuqiuDetail,
  });

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        const ColoredBox(color: AppColors.stage),
        Live2dView(
          key: live2dKey,
          expression: expression,
          isSpeaking: isSpeaking,
          motion: motion,
        ),
        Positioned(
          top: AppSpacing.md,
          left: AppSpacing.md,
          right: AppSpacing.sm,
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: _MatchStatusCarousel(items: match.statusCarouselItems),
              ),
              const SizedBox(width: AppSpacing.xs),
              _ConnectionMark(status: socketStatus),
            ],
          ),
        ),
        if (subtitlesEnabled)
          Positioned(
            left: AppSpacing.md,
            right: AppSpacing.sm,
            bottom: AppSpacing.md,
            child: Semantics(
              liveRegion: true,
              label: '球球说：$qiuqiuLine $qiuqiuDetail',
              child: DecoratedBox(
                decoration: BoxDecoration(
                  color: AppColors.night.withValues(alpha: 0.82),
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Padding(
                  padding: const EdgeInsets.all(AppSpacing.sm),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        qiuqiuLine,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.headlineMedium,
                      ),
                      if (qiuqiuDetail.isNotEmpty) ...[
                        const SizedBox(height: AppSpacing.xs),
                        Text(
                          qiuqiuDetail,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: Theme.of(
                            context,
                          )
                              .textTheme
                              .bodyMedium
                              ?.copyWith(color: AppColors.ink),
                        ),
                      ],
                    ],
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }
}

class _MatchStatusCarousel extends StatefulWidget {
  final List<String> items;

  const _MatchStatusCarousel({required this.items});

  @override
  State<_MatchStatusCarousel> createState() => _MatchStatusCarouselState();
}

class _MatchStatusCarouselState extends State<_MatchStatusCarousel>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;
  Timer? _timer;
  int _index = 0;
  int _previousIndex = 0;
  bool _reduceMotion = false;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 260),
    );
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final reduceMotion =
        MediaQuery.maybeOf(context)?.disableAnimations ?? false;
    if (_reduceMotion != reduceMotion) {
      _reduceMotion = reduceMotion;
      _restartTimer();
    } else if (_timer == null) {
      _restartTimer();
    }
  }

  @override
  void didUpdateWidget(_MatchStatusCarousel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!listEquals(widget.items, oldWidget.items)) {
      _index = 0;
      _previousIndex = 0;
      _controller.reset();
      _restartTimer();
    }
  }

  void _restartTimer() {
    _timer?.cancel();
    _timer = null;
    if (_reduceMotion || widget.items.length < 2) return;
    _timer = Timer.periodic(const Duration(milliseconds: 4500), (_) {
      if (!mounted || widget.items.length < 2) return;
      setState(() {
        _previousIndex = _index;
        _index = (_index + 1) % widget.items.length;
      });
      _controller.forward(from: 0);
    });
  }

  @override
  Widget build(BuildContext context) {
    final items = widget.items.isEmpty ? const ['等待比赛动态'] : widget.items;
    final currentIndex = _index.clamp(0, items.length - 1);
    final previousIndex = _previousIndex.clamp(0, items.length - 1);
    final textStyle = Theme.of(context).textTheme.labelLarge?.copyWith(
          color: AppColors.ink,
          height: 1.35,
          fontWeight: FontWeight.w600,
        );

    Widget statusText(String value) => Align(
          alignment: Alignment.centerLeft,
          child: Text(
            value,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: textStyle,
          ),
        );

    return Align(
      alignment: Alignment.topLeft,
      child: Semantics(
        liveRegion: true,
        label: '比赛动态：${items[currentIndex]}',
        child: Container(
          constraints: const BoxConstraints(maxWidth: 320),
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.sm,
            vertical: AppSpacing.xs,
          ),
          decoration: BoxDecoration(
            color: AppColors.night.withValues(alpha: 0.78),
            border: Border.all(color: AppColors.line),
            borderRadius: BorderRadius.circular(6),
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const DecoratedBox(
                decoration: BoxDecoration(
                  color: AppColors.orange,
                  shape: BoxShape.circle,
                ),
                child: SizedBox.square(dimension: 7),
              ),
              const SizedBox(width: AppSpacing.xs),
              Flexible(
                child: SizedBox(
                  height: 38,
                  child: _reduceMotion
                      ? statusText(items[currentIndex])
                      : AnimatedBuilder(
                          animation: _controller,
                          builder: (context, child) {
                            if (_controller.isDismissed ||
                                previousIndex == currentIndex) {
                              return statusText(items[currentIndex]);
                            }
                            final progress = Curves.easeOutQuart.transform(
                              _controller.value,
                            );
                            return ClipRect(
                              child: Stack(
                                children: [
                                  Opacity(
                                    opacity: 1 - progress,
                                    child: Transform.translate(
                                      offset: Offset(0, -18 * progress),
                                      child: statusText(items[previousIndex]),
                                    ),
                                  ),
                                  Opacity(
                                    opacity: progress,
                                    child: Transform.translate(
                                      offset: Offset(0, 18 * (1 - progress)),
                                      child: statusText(items[currentIndex]),
                                    ),
                                  ),
                                ],
                              ),
                            );
                          },
                        ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  void dispose() {
    _timer?.cancel();
    _controller.dispose();
    super.dispose();
  }
}

class _ConversationDock extends StatelessWidget {
  final ConversationPhase phase;
  final bool continuousEnabled;
  final String userLine;
  final String? notice;
  final bool textMode;
  final TextEditingController textController;
  final VoidCallback onToggleContinuous;
  final VoidCallback onOpenSettings;
  final VoidCallback onOpenText;
  final VoidCallback onCloseText;
  final VoidCallback onSendText;
  final Future<void> Function() onMicDown;
  final VoidCallback onMicUp;

  const _ConversationDock({
    required this.phase,
    required this.continuousEnabled,
    required this.userLine,
    required this.notice,
    required this.textMode,
    required this.textController,
    required this.onToggleContinuous,
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
      color: AppColors.paper,
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
                          color: AppColors.paperInk,
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
                ).textTheme.bodyMedium?.copyWith(color: AppColors.paperInk),
              ),
            ] else if (userLine.isNotEmpty) ...[
              const SizedBox(height: AppSpacing.xs),
              Text(
                '「$userLine」',
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.bodyLarge?.copyWith(
                      color: AppColors.paperInk,
                      fontWeight: FontWeight.w600,
                    ),
              ),
            ],
            const SizedBox(height: AppSpacing.sm),
            const Divider(height: 1, color: AppColors.paperInk),
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
                          color: AppColors.paperInk,
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
      case ConversationPhase.welcoming:
        return AppColors.orangeDeep;
      case ConversationPhase.listening:
      case ConversationPhase.userSpeaking:
        return AppColors.green;
      case ConversationPhase.understanding:
        return AppColors.yellow;
      case ConversationPhase.speaking:
        return AppColors.orangeDeep;
      case ConversationPhase.permissionDenied:
      case ConversationPhase.offline:
      case ConversationPhase.failed:
        return AppColors.red;
      case ConversationPhase.idle:
        return AppColors.muted;
    }
  }

  String get _phaseLabel {
    switch (phase) {
      case ConversationPhase.welcoming:
        return '第一次见面 · 球球正在和你打招呼';
      case ConversationPhase.listening:
        return continuousEnabled ? '连续对话已开启 · 你直接说' : '正在听你说';
      case ConversationPhase.userSpeaking:
        return '听到你在说话 · 继续说';
      case ConversationPhase.understanding:
        return '听见了 · 正在结合比赛想一想';
      case ConversationPhase.speaking:
        return '球球正在回答 · 你可以随时插话';
      case ConversationPhase.permissionDenied:
        return '麦克风还没有权限';
      case ConversationPhase.offline:
        return '暂时离线 · 字幕和比赛画面仍保留';
      case ConversationPhase.failed:
        return '这一句没有听清';
      case ConversationPhase.idle:
        return continuousEnabled ? '准备好后直接说' : '按住麦克风说话';
    }
  }
}

class _VoiceOrb extends StatelessWidget {
  final ConversationPhase phase;
  final bool continuousEnabled;

  const _VoiceOrb({required this.phase, required this.continuousEnabled});

  @override
  Widget build(BuildContext context) {
    final background = switch (phase) {
      ConversationPhase.welcoming => AppColors.orangeDeep,
      ConversationPhase.listening => AppColors.yellow,
      ConversationPhase.userSpeaking => AppColors.green,
      ConversationPhase.understanding => AppColors.paperInk,
      ConversationPhase.speaking => AppColors.orangeDeep,
      ConversationPhase.permissionDenied ||
      ConversationPhase.offline ||
      ConversationPhase.failed =>
        AppColors.red,
      ConversationPhase.idle => AppColors.orangeDeep,
    };
    final foreground = phase == ConversationPhase.listening
        ? AppColors.paperInk
        : Colors.white;
    final label = switch (phase) {
      ConversationPhase.welcoming => '你好呀',
      ConversationPhase.listening => '聆听中',
      ConversationPhase.userSpeaking => '你在说',
      ConversationPhase.understanding => '想一想',
      ConversationPhase.speaking => '球球在说',
      ConversationPhase.permissionDenied => '没权限',
      ConversationPhase.offline => '离线',
      ConversationPhase.failed => '再试一次',
      ConversationPhase.idle => continuousEnabled ? '直接说' : '按住说',
    };
    return Semantics(
      button: true,
      label: continuousEnabled ? '语音状态：$label' : '按住和球球说话',
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 180),
        curve: Curves.easeOutQuart,
        width: 80,
        height: 80,
        decoration: BoxDecoration(
          color: background,
          shape: BoxShape.circle,
          border: Border.all(color: AppColors.paperInk, width: 2),
          boxShadow: const [
            BoxShadow(color: AppColors.paperInk, offset: Offset(5, 5)),
          ],
        ),
        alignment: Alignment.center,
        child: Text(
          label,
          textAlign: TextAlign.center,
          style: Theme.of(context).textTheme.labelMedium?.copyWith(
                color: foreground,
                fontWeight: FontWeight.w800,
              ),
        ),
      ),
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
          foregroundColor: AppColors.paperInk,
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
          color: AppColors.paperInk,
          icon: const Icon(Icons.mic_rounded),
        ),
        const SizedBox(width: AppSpacing.xs),
        Expanded(
          child: TextField(
            controller: controller,
            autofocus: true,
            style: const TextStyle(color: AppColors.paperInk),
            textInputAction: TextInputAction.send,
            onSubmitted: (_) => onSend(),
            decoration: const InputDecoration(
              isDense: true,
              filled: true,
              fillColor: Colors.white,
              hintText: '直接和球球说…',
              hintStyle: TextStyle(color: Color(0xFF665F58)),
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

class _ConnectionMark extends StatelessWidget {
  final SocketStatus status;
  final bool dark;

  const _ConnectionMark({required this.status, this.dark = false});

  @override
  Widget build(BuildContext context) {
    final connected = status == SocketStatus.connected;
    return Semantics(
      label: connected ? '比赛已连接' : '比赛正在连接',
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: connected ? AppColors.green : AppColors.yellow,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: AppSpacing.xxs),
          Text(
            connected ? '现场' : '连接中',
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
                  color: dark ? AppColors.paperInk : AppColors.ink,
                ),
          ),
        ],
      ),
    );
  }
}

class PendingAudio {
  final String mime;
  final String? traceId;
  final int? byteLength;

  const PendingAudio({required this.mime, this.traceId, this.byteLength});
}

Map<String, dynamic>? mutedPlaybackReceipt(PendingAudio metadata) {
  final traceId = metadata.traceId?.trim();
  if (traceId == null || traceId.isEmpty) return null;
  return {
    'type': 'voice_playback',
    'traceId': traceId,
    'state': 'skipped',
  };
}

class PendingAudioQueue {
  final Queue<PendingAudio> _items = Queue<PendingAudio>();

  void add(PendingAudio metadata) => _items.addLast(metadata);

  PendingAudio? take() => _items.isEmpty ? null : _items.removeFirst();

  void clear() => _items.clear();
}

@immutable
class MatchViewData {
  final String homeTeam;
  final String awayTeam;
  final int homeScore;
  final int awayScore;
  final String competition;
  final String period;
  final String clock;
  final List<String> recentEventLabels;

  const MatchViewData({
    this.homeTeam = '利物浦',
    this.awayTeam = '切尔西',
    this.homeScore = 2,
    this.awayScore = 1,
    this.competition = '欧冠',
    this.period = '下半场',
    this.clock = "78'",
    this.recentEventLabels = const [],
  });

  String get liveLabel => switch (period.trim().toLowerCase()) {
        '' || 'pre_match' => '等待开赛',
        'finished' || 'full_time' => '已结束',
        _ => '直播中',
      };

  String get eventLabel => statusCarouselItems.first;

  List<String> get statusCarouselItems {
    final items = <String>[];
    for (final event in recentEventLabels) {
      final normalized = event.trim();
      if (normalized.isNotEmpty && !items.contains(normalized)) {
        items.add(normalized);
      }
    }
    final phaseLabel = _displayPeriod(period);
    final scoreLabel = '$homeTeam $homeScore—$awayScore $awayTeam';
    final currentState = '$phaseLabel · $scoreLabel';
    if (!items.contains(currentState)) items.add(currentState);
    final clockLabel = clock.trim();
    if (clockLabel.isNotEmpty) {
      final timing = [
        clockLabel,
        if (competition.trim().isNotEmpty) competition.trim(),
        liveLabel,
      ].join(' · ');
      if (!items.contains(timing)) items.add(timing);
    }
    return List.unmodifiable(items);
  }

  MatchViewData withSnapshot(Map<String, dynamic> snapshot) {
    final score = _map(snapshot['score']);
    final events = snapshot['recentEvents'];
    final eventLabels = events is List
        ? events
            .map(_map)
            .whereType<Map<String, dynamic>>()
            .map(_eventDescription)
            .take(4)
            .toList(growable: false)
        : const <String>[];
    return copyWith(
      homeTeam: snapshot['homeTeam'] as String?,
      awayTeam: snapshot['awayTeam'] as String?,
      homeScore: _integer(score?['home']),
      awayScore: _integer(score?['away']),
      period: snapshot['period'] as String?,
      clock: snapshot['clock'] as String?,
      recentEventLabels: eventLabels,
    );
  }

  MatchViewData withEvent(Map<String, dynamic> event) {
    final score = _map(event['score']);
    final eventLabel = _eventDescription(event);
    final eventLabels = [
      eventLabel,
      ...recentEventLabels.where((item) => item != eventLabel),
    ].take(4).toList(growable: false);
    return copyWith(
      homeScore: _integer(score?['home']),
      awayScore: _integer(score?['away']),
      period: event['period'] as String?,
      clock: event['clock'] as String?,
      recentEventLabels: eventLabels,
    );
  }

  MatchViewData withLegacyScore(String score) {
    final parts = score.split(RegExp(r'[-—:]'));
    if (parts.length != 2) return this;
    return copyWith(
      homeScore: int.tryParse(parts.first.trim()),
      awayScore: int.tryParse(parts.last.trim()),
    );
  }

  MatchViewData copyWith({
    String? homeTeam,
    String? awayTeam,
    int? homeScore,
    int? awayScore,
    String? competition,
    String? period,
    String? clock,
    List<String>? recentEventLabels,
  }) {
    return MatchViewData(
      homeTeam: homeTeam?.trim().isNotEmpty == true ? homeTeam! : this.homeTeam,
      awayTeam: awayTeam?.trim().isNotEmpty == true ? awayTeam! : this.awayTeam,
      homeScore: homeScore ?? this.homeScore,
      awayScore: awayScore ?? this.awayScore,
      competition: competition ?? this.competition,
      period: period?.trim().isNotEmpty == true ? period! : this.period,
      clock: clock?.trim().isNotEmpty == true ? clock! : this.clock,
      recentEventLabels: recentEventLabels ?? this.recentEventLabels,
    );
  }
}

Map<String, dynamic>? _map(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return value.cast<String, dynamic>();
  return null;
}

int? _integer(dynamic value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '');
}

String _displayPeriod(String value) {
  return switch (value.trim().toLowerCase()) {
    '' || 'pre_match' => '赛前',
    'first_half' => '上半场',
    'half_time' => '中场休息',
    'second_half' => '下半场',
    'extra_time' => '加时赛',
    'penalties' => '点球大战',
    'finished' || 'full_time' => '全场结束',
    _ => value.trim(),
  };
}

String _eventDescription(Map<String, dynamic> event) {
  final description = event['description'] as String?;
  if (description != null && description.trim().isNotEmpty) {
    return '刚刚 · ${description.trim()}';
  }
  final player = event['playerName'] as String?;
  final eventType = event['eventType'] as String? ?? '';
  final label = switch (eventType) {
    'goal' => '进球',
    'yellow_card' => '黄牌',
    'red_card' => '红牌',
    'penalty' => '点球',
    'match_start' => '比赛开始',
    'match_end' => '比赛结束',
    _ => '比赛有新进展',
  };
  return player?.trim().isNotEmpty == true
      ? '刚刚 · ${player!.trim()}$label'
      : '刚刚 · $label';
}
