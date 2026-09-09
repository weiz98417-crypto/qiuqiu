import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;
import 'package:flutter/material.dart';

export '../services/match_view_data.dart';

import '../services/audio_player.dart';
import '../services/preferences_service.dart';
import '../services/recorder_stub.dart';
import '../services/session_service.dart';
import '../services/match_session_controller.dart';
import '../services/match_view_data.dart';
import '../services/streaming_transcription.dart';
import '../services/websocket_service.dart';
import '../theme/app_theme.dart';
import '../widgets/live2d_view.dart';
import '../widgets/match_actions_menu.dart';
import '../widgets/reply_subtitle_card.dart';
import 'reply_display.dart';
import 'settings_screen.dart';

class MatchScreen extends StatefulWidget {
  final String? matchId;
  final VoidCallback? onExit;
  final bool autoEnter;
  final MatchViewData? initialMatch;

  const MatchScreen({
    super.key,
    this.matchId,
    this.onExit,
    this.autoEnter = false,
    this.initialMatch,
  });

  @override
  State<MatchScreen> createState() => _MatchScreenState();
}

class _MatchScreenState extends State<MatchScreen> {
  static const _configuredSocketUrl = String.fromEnvironment('QIUQIU_WS_URL');

  final WebSocketService _socket = WebSocketService();
  final AudioPlayerService _audio = AudioPlayerService();
  final PreferencesService _preferences = PreferencesService();
  final SessionService _sessions = SessionService();
  late final MatchSessionController _sessionController;
  final VADService _vad = VADService();
  final StreamingTranscription _streamingTranscription =
      StreamingTranscription();
  final TextEditingController _textController = TextEditingController();
  final GlobalKey<Live2dViewState> _live2dKey = GlobalKey<Live2dViewState>();
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  late final Future<void> _profileLoad;

  UserProfile _profile = const UserProfile(
    nickname: '',
    favoriteTeam: '利物浦',
    talkativeness: 'normal',
  );
  bool _sessionRefreshScheduled = false;
  bool _sessionRefreshDirty = false;
  bool _textMode = false;
  bool _isHoldingToTalk = false;
  String _userId = '';
  int _signalSequence = 0;
  Timer? _presentationReturnTimer;
  Timer? _clockTicker;
  String _deviceId = '';

  bool get _continuousEnabled => _profile.continuousConversation;
  bool get _insideMatch =>
      _sessionController.state.presence == MatchSessionPresence.active;
  bool get _allowPop =>
      _sessionController.state.presence == MatchSessionPresence.left;

  MatchSessionPhase get _phase => _sessionController.state.phase;

  bool get _firstMeetingCompleted =>
      _sessionController.state.firstMeetingCompleted;

  String get _expression => _sessionController.state.expression;

  String? get _motion => _sessionController.state.motion;

  String? get _notice => _sessionController.state.notice;

  String get _qiuqiuLine => _sessionController.state.replyText;
  String get _qiuqiuDetail => _sessionController.state.replyDetail;
  String get _userLine => _sessionController.state.userText;
  bool get _forceSubtitleFallback => _sessionController.state.subtitleFallback;
  MatchViewData get _match => _sessionController.state.match;
  CompanionPresentation? get _activePresentation =>
      _sessionController.state.activePresentation;

  SocketStatus get _socketStatus => SocketStatus.values.firstWhere(
        (status) => status.name == _sessionController.state.transportStatus,
        orElse: () => SocketStatus.connecting,
      );

  String _nextSignalId() {
    _signalSequence += 1;
    return 'turn_${_userId}_${DateTime.now().microsecondsSinceEpoch}_$_signalSequence';
  }

  @override
  void initState() {
    super.initState();
    _sessionController = MatchSessionController(
      initialMatch: widget.initialMatch ?? const MatchViewData(),
    );
    _sessionController.addListener(_onSessionStateChanged);
    _bindServices();
    _clockTicker = Timer.periodic(const Duration(milliseconds: 250), (_) {
      if (!mounted) return;
      _sessionController.tickClock(DateTime.now().toUtc());
    });
    _profileLoad = _initialize();
    unawaited(_profileLoad);
  }

  void _bindServices() {
    _socket.setRefreshTokenCallback(_refreshSessionToken);
    _subscriptions.addAll([
      _socket.onMessage.listen(_handleSocketMessage),
      _socket.onBinary.listen(_handleAudioBytes),
      _socket.statusStream.listen(_handleSocketStatus),
      _vad.events.listen(_handleVadEvent),
      _vad.audioChunks.listen(_handleAudioChunk),
      _vad.inputDevices.listen(_handleAudioInputDevices),
      _audio.stateStream.listen(_handleAudioState),
    ]);
  }

  void _handleAudioInputDevices(List<AudioInputDevice> devices) {
    if (!mounted) return;
    _sessionController.setAudioInputs(
      devices
          .map((device) =>
              AudioInputViewData(id: device.id, label: device.label))
          .toList(growable: false),
      selectedId: _vad.selectedInputDeviceId,
      activeId: _vad.activeInputDeviceId,
    );
  }

  String get _audioInputLabel => _sessionController.audioInputLabel;
  String get _selectedAudioInputId =>
      _sessionController.state.selectedAudioInputId;

  String _socketUrl() {
    final requestedMatchId = widget.matchId?.trim();
    if (_configuredSocketUrl.isNotEmpty) {
      final configured = Uri.parse(_configuredSocketUrl);
      if (requestedMatchId == null || requestedMatchId.isEmpty) {
        return _configuredSocketUrl;
      }
      return configured
          .replace(path: '/ws/match/${Uri.encodeComponent(requestedMatchId)}')
          .toString();
    }
    if (kIsWeb) {
      final page = Uri.base;
      final matchId = requestedMatchId == null || requestedMatchId.isEmpty
          ? (page.queryParameters['matchId']?.trim().isNotEmpty == true
              ? page.queryParameters['matchId']!.trim()
              : 'test')
          : requestedMatchId;
      return Uri(
        scheme: page.scheme == 'https' ? 'wss' : 'ws',
        userInfo: page.userInfo,
        host: page.host,
        port: page.hasPort ? page.port : null,
        path: '/ws/match/${Uri.encodeComponent(matchId)}',
      ).toString();
    }
    final matchId = requestedMatchId == null || requestedMatchId.isEmpty
        ? 'test'
        : requestedMatchId;
    return 'ws://10.0.2.2:8080/ws/match/${Uri.encodeComponent(matchId)}';
  }

  Future<void> _initialize() async {
    final profile = await _preferences.load();
    final deviceId = await _preferences.loadOrCreateAnonymousUserId();
    _deviceId = deviceId;
    final firstMeetingCompleted = await _preferences.hasCompletedFirstMeeting();
    await _audio.setMuted(!profile.soundEnabled);
    if (!mounted) return;
    setState(() {
      _profile = profile;
    });
    _sessionController.setFirstMeetingCompleted(firstMeetingCompleted);
    try {
      final session = await _sessions.ensureSession(
        baseUrl: normalizeAPIBaseURL(_socketUrl()),
        deviceId: deviceId,
      );
      if (!mounted) return;
      setState(() {
        _userId = session.userId;
      });
      _socket.connect(_socketUrl(), token: session.accessToken);
    } catch (_) {
      if (!mounted) return;
      _sessionController.initializationFailed();
    }
  }

  Future<String?> _refreshSessionToken() async {
    if (_deviceId.isEmpty) return null;
    try {
      final session = await _sessions.ensureSession(
        baseUrl: normalizeAPIBaseURL(_socketUrl()),
        deviceId: _deviceId,
      );
      if (mounted) {
        setState(() {
          _userId = session.userId;
        });
      }
      return session.accessToken;
    } catch (_) {
      return null;
    }
  }

  void _handleSocketStatus(SocketStatus status) {
    if (!mounted) return;
    if (status != SocketStatus.connected) {
      for (final fallback in _streamingTranscription.cancelAll(_socket.send)) {
        _sessionController.queueReconnectVoiceFallback(
          ReconnectVoiceFallback(
            audio: fallback.audio,
            signalId: fallback.signalId,
          ),
        );
      }
    }
    _sessionController.dispatch(SocketSessionEvent(
      connected: status == SocketStatus.connected,
      failed: status == SocketStatus.failed,
    ));
    _runSessionCommands();
    if (status == SocketStatus.connected) {
      if (widget.autoEnter && !_insideMatch) {
        unawaited(_enterMatch());
      }
    }
  }

  void _handleSocketMessage(Map<String, dynamic> message) {
    if (!mounted) return;
    final type = message['type'] as String? ?? '';
    switch (type) {
      case 'transcript_partial':
      case 'transcript_final':
      case 'transcript_error':
        _handleTranscriptMessage(message);
        break;
      case 'match_snapshot':
        final snapshot = _map(message['data']);
        if (snapshot != null) {
          _sessionController.dispatch(
            MatchSnapshotSessionEvent(snapshot, DateTime.now().toUtc()),
          );
        }
        break;
      case 'match_clock':
        final clock = MatchClockViewData.tryParse(_map(message['data']));
        if (clock != null) {
          _sessionController.dispatch(MatchClockSessionEvent(
            _map(message['data'])!,
            DateTime.now().toUtc(),
          ));
        }
        break;
      case 'match_event':
        final event = _map(message['data']);
        final snapshot = _map(message['snapshot']);
        final deliveryKey =
            message['deliveryKey']?.toString() ?? matchEventDeliveryKey(event);
        _sessionController.dispatch(MatchEventProjectionSessionEvent(
          event: event,
          snapshot: snapshot,
          deliveryKey: deliveryKey,
          now: DateTime.now().toUtc(),
        ));
        if (event != null) {
          _sessionController.dispatch(MatchFactSessionEvent(
            event['factId']?.toString() ?? event['id']?.toString() ?? '',
            _integer(event['factRevision']) ?? 0,
          ));
        }
        break;
      case 'match_fact_retracted':
        _handleMatchFactRetracted(message);
        break;
      case 'presentation':
        final presentation = CompanionPresentation.fromReplyData({
          'presentation': _map(message['data']),
        });
        if (presentation != null) {
          _sessionController.receivePresentation(
            presentation: presentation,
            source: message['source']?.toString(),
            eventId: message['eventId']?.toString(),
            deliveryKey: message['deliveryKey']?.toString(),
          );
          _runSessionCommands();
        }
        break;
      case 'event':
        _handleLegacyEvent(message);
        break;
      case 'expression':
        final expression = CompanionPresentation.normalizeExpression(
          message['state'] as String?,
        );
        if (expression == null) break;
        _sessionController.receiveLegacyExpression(
          expression,
          _motionForExpression(expression),
        );
        _runSessionCommands();
        break;
      case 'voice_audio':
        _sessionController.queueAudioMetadata(
            PendingAudio(
              mime: message['mime'] as String? ?? 'audio/wav',
              traceId: message['traceId'] as String?,
              eventId: message['eventId']?.toString(),
              deliveryKey: message['deliveryKey']?.toString(),
              byteLength: _integer(message['byteLength']),
            ),
            source: message['source']?.toString());
        break;
      case 'voice_status':
        _handleVoiceStatus(message);
        break;
      case 'first_meeting_status':
        _handleFirstMeetingStatus(message);
        break;
      case 'interrupt':
        _sessionController.serverInterrupted();
        _runSessionCommands();
        break;
      case 'reconnecting':
        _sessionController.setNotice('正在回到比赛现场…');
        break;
    }
  }

  void _handleLegacyEvent(Map<String, dynamic> message) {
    final eventType = message['event'] as String? ?? '';
    final data = _map(message['data']);
    if (eventType == 'qiuqiu_reply') {
      final reply = data?['text'] as String?;
      if (reply == null || reply.trim().isEmpty) return;
      final source = data?['source']?.toString();
      final eventId = data?['eventId']?.toString();
      final traceId = data?['traceId'] as String?;
      final presentation = CompanionPresentation.fromReplyData(data);
      final parts = splitReplyForDisplay(reply.trim());
      _sessionController.receiveReply(
        text: parts.$1,
        detail: parts.$2,
        source: source,
        eventId: eventId,
        deliveryKey: data?['deliveryKey']?.toString(),
        traceId: traceId,
        presentation: presentation,
      );
      _runSessionCommands();
      return;
    }

    _sessionController.dispatch(LegacyMatchSessionEvent(
      score: data?['score'] is String ? data!['score'] as String : null,
      minute: data?['minute'],
    ));
    _sessionController.receiveLegacyEventAnimation(
      _expressionForEvent(eventType),
      _motionForEvent(eventType),
    );
  }

  void _handleMatchFactRetracted(Map<String, dynamic> message) {
    final data = _map(message['data']);
    final eventIds = [
      data?['eventId']?.toString(),
      data?['factId']?.toString(),
    ]
        .whereType<String>()
        .map((value) => value.trim())
        .where((value) => value.isNotEmpty)
        .toSet();
    if (eventIds.isEmpty) return;
    final factId = data?['factId']?.toString().trim() ?? '';
    if (factId.isNotEmpty) {
      _sessionController.dispatch(MatchFactSessionEvent(
        factId,
        _integer(data?['factRevision']) ??
            (_sessionController.state.factRevisions[factId] ?? 0),
        retracted: true,
        relatedIds: eventIds,
        continuousEnabled: _continuousEnabled,
      ));
      _runSessionCommands();
    }
  }

  void _handleVoiceStatus(Map<String, dynamic> message) {
    final state = message['state'] as String? ?? '';
    _sessionController.handleVoiceStatus(
      state,
      continuousEnabled: _continuousEnabled,
    );
    _runSessionCommands();
  }

  void _handleFirstMeetingStatus(Map<String, dynamic> message) {
    final state = message['state'] as String? ?? '';
    _sessionController.firstMeetingStatus(
      state,
      continuousEnabled: _continuousEnabled,
    );
    _runSessionCommands();
  }

  void _handleAudioBytes(Uint8List audioBytes) {
    _sessionController.consumeAudio(
      audioBytes,
      soundEnabled: _profile.soundEnabled,
      continuousEnabled: _continuousEnabled,
    );
    _runSessionCommands();
  }

  void _startStreamingCapture() {
    if (_userId.trim().isEmpty) return;
    final signalId = _nextSignalId();
    _streamingTranscription.startCapture(
      utteranceId: 'utterance_$signalId',
      signalId: signalId,
      userId: _userId,
      send: _socket.send,
    );
  }

  void _handleAudioChunk(Uint8List audio) {
    _streamingTranscription.append(audio, _socket.send);
  }

  void _handleTranscriptMessage(Map<String, dynamic> message) {
    final update = _streamingTranscription.accept(message);
    if (update == null || !mounted) return;
    final activeUtteranceId = _streamingTranscription.activeUtteranceId;
    final anotherUtteranceIsBeingSpoken =
        _phase == MatchSessionPhase.userSpeaking &&
            activeUtteranceId != null &&
            activeUtteranceId != update.utteranceId;
    switch (update.kind) {
      case TranscriptUpdateKind.partialTranscript:
        _sessionController.transcriptPartial(
          update.text,
          anotherUtteranceActive: anotherUtteranceIsBeingSpoken,
        );
        break;
      case TranscriptUpdateKind.finalTranscript:
        _sessionController.transcriptFinal(
          update.text,
          userStillSpeaking: _phase == MatchSessionPhase.userSpeaking,
        );
        break;
      case TranscriptUpdateKind.recoverableError:
        _sessionController.transcriptRecoverableError(
          anotherUtteranceActive: anotherUtteranceIsBeingSpoken,
        );
        break;
      case TranscriptUpdateKind.fallback:
        final fallbackAudio = update.fallbackAudio;
        if (fallbackAudio != null && fallbackAudio.isNotEmpty) {
          _submitLegacyVoice(
            fallbackAudio,
            preserveCurrentCapture: _phase == MatchSessionPhase.userSpeaking,
            signalId: update.fallbackSignalId,
          );
        } else {
          _sessionController.transcriptFallbackUnavailable();
        }
        break;
    }
  }

  bool _submitLegacyVoice(
    Uint8List audio, {
    bool preserveCurrentCapture = false,
    bool queueIfOffline = true,
    String? signalId,
  }) {
    if (audio.isEmpty) return false;
    final turnSignalId = signalId ?? _nextSignalId();
    final sent = _socket.send({
      'type': 'user_speech',
      'userId': _userId,
      'signalId': turnSignalId,
      'text': '',
      'mode': 'voice',
      'audio': base64Encode(audio),
      'talkativeness': _profile.talkativeness,
    });
    _sessionController.voiceSubmitted(
      sent: sent,
      preserveCurrentCapture: preserveCurrentCapture,
    );
    if (!sent && queueIfOffline) {
      _sessionController.queueReconnectVoiceFallback(
        ReconnectVoiceFallback(audio: audio, signalId: turnSignalId),
      );
    }
    return sent;
  }

  void _handleVadEvent(VADEvent event) {
    if (!mounted) return;
    switch (event.state) {
      case VADState.listening:
        _sessionController.vadListening(
          hasPendingTranscript: _streamingTranscription.hasPendingFinal,
        );
        _runSessionCommands();
        break;
      case VADState.speaking:
        _sessionController.vadSpeaking(
          continuousEnabled: _continuousEnabled,
        );
        _runSessionCommands();
        break;
      case VADState.sentenceEnd:
        final audio = _vad.drainAudio();
        final finish = _streamingTranscription.finish(audio, _socket.send);
        if (finish.streamed) {
          _sessionController.vadSentenceStreamed();
        } else if (finish.fallbackAudio case final fallbackAudio?) {
          _submitLegacyVoice(
            fallbackAudio,
            signalId: finish.fallbackSignalId,
          );
        }
        break;
      case VADState.idle:
        _sessionController.vadIdle();
        _runSessionCommands();
        break;
      case VADState.permissionDenied:
        _sessionController.vadPermissionDenied();
        _runSessionCommands();
        break;
      case VADState.failure:
        _sessionController.vadFailed();
        _runSessionCommands();
        break;
    }
  }

  void _handleAudioState(AudioState state) {
    if (!mounted) return;
    if (state.status == AudioPlaybackStatus.failed) {
      debugPrint('Audio playback failed: ${state.error}');
    }
    _sessionController.dispatch(PlaybackSessionEvent(
      switch (state.status) {
        AudioPlaybackStatus.started => 'started',
        AudioPlaybackStatus.ended => 'ended',
        AudioPlaybackStatus.interrupted => 'interrupted',
        AudioPlaybackStatus.blocked => 'blocked',
        AudioPlaybackStatus.failed => 'failed',
      },
      traceId: state.traceId,
      continuousEnabled: _continuousEnabled,
      error: state.error,
    ));
    _runSessionCommands();
  }

  Future<void> _enterMatch() async {
    if (!_sessionController.beginEntry()) return;
    await _profileLoad;
    if (!mounted || _socketStatus != SocketStatus.connected) {
      _sessionController.cancelEntry();
      return;
    }
    _sessionController.entered();
    _sessionController.openSession(_userId);
    _runSessionCommands();
    if (!_firstMeetingCompleted) {
      final sent = _socket.send({
        'type': 'first_meeting',
        'userId': _userId,
        'nickname': _profile.nickname,
        'favoriteTeam': _profile.favoriteTeam,
      });
      _sessionController.firstMeetingRequested(
        sent: sent,
        continuousEnabled: _continuousEnabled,
      );
      _runSessionCommands();
      if (sent) return;
    } else if (_continuousEnabled) {
      await _vad.startListening(VADMode.freeTalk);
    }
  }

  Future<void> _leaveMatch({bool confirm = true}) async {
    final requiresConfirmation =
        _sessionController.leaveRequiresConfirmation(confirm);
    if (!mounted || !_sessionController.beginLeaving()) return;
    final leave =
        requiresConfirmation ? await showMatchExitDialog(context) : true;
    if (leave != true || !mounted) {
      _sessionController.cancelLeaving();
      return;
    }
    _vad.stopListening();
    if (_sessionController.leavingActiveSession) {
      _socket.send({
        'type': 'session_closed',
        'userId': _userId,
        'reason': 'user_left',
      });
    }
    await _audio.pause();
    _sessionController.dispatch(const AudioQueueClearedSessionEvent());
    _runSessionCommands();
    _streamingTranscription.cancelAll(_socket.send);
    await _socket.dispose();
    _sessionController.left();
    await _returnToCatalog();
  }

  Future<void> _returnToCatalog() async {
    if (!mounted) return;
    await WidgetsBinding.instance.endOfFrame;
    if (!mounted) return;
    if (widget.onExit != null) {
      widget.onExit!.call();
      if (!mounted) return;
      final navigator = Navigator.of(context);
      if (ModalRoute.of(context)?.isCurrent ?? false) {
        if (navigator.canPop()) {
          navigator.pop();
        } else {
          _sessionController.cancelLeaving();
        }
      }
      return;
    }
    Navigator.of(context).pop();
  }

  void _reconnect() {
    if (!_sessionController.beginManualReconnect()) return;
    _runSessionCommands();
  }

  void _onSessionStateChanged() {
    if (!mounted) return;
    _sessionRefreshDirty = true;
    if (_sessionRefreshScheduled) return;
    _sessionRefreshScheduled = true;
    void flushSessionRefresh(Duration _) {
      _sessionRefreshScheduled = false;
      if (!mounted) {
        _sessionRefreshDirty = false;
        return;
      }
      if (!_sessionRefreshDirty) return;
      _sessionRefreshDirty = false;
      setState(() {});
      if (_sessionRefreshDirty && mounted) {
        _sessionRefreshScheduled = true;
        WidgetsBinding.instance.addPostFrameCallback(flushSessionRefresh);
        WidgetsBinding.instance.scheduleFrame();
      }
    }

    WidgetsBinding.instance.addPostFrameCallback(flushSessionRefresh);
    WidgetsBinding.instance.scheduleFrame();
  }

  Future<void> _reconnectWithFreshSession() async {
    final token = await _refreshSessionToken();
    if (!mounted) return;
    if (token == null || token.isEmpty) {
      _sessionController.manualReconnectFailed();
      return;
    }
    _socket.connect(_socketUrl(), token: token);
  }

  Future<void> _switchMatch() async {
    if (!mounted ||
        _sessionController.state.presence == MatchSessionPresence.leaving) {
      return;
    }
    final confirmed = await showMatchSwitchDialog(context);
    if (confirmed == true) await _leaveMatch(confirm: false);
  }

  Future<void> _toggleContinuous() async {
    final enabled = !_continuousEnabled;
    final updated = _profile.copyWith(continuousConversation: enabled);
    setState(() {
      _profile = updated;
    });
    _sessionController.setNotice(
      enabled ? null : '连续对话已关闭，按住麦克风仍能说话。',
    );
    await _preferences.save(updated);
    if (enabled) {
      await _vad.startListening(VADMode.freeTalk);
    } else {
      _vad.stopListening();
      await _audio.pause();
    }
  }

  Future<void> _startPushToTalk() async {
    if (_isHoldingToTalk) return;
    if (_continuousEnabled) {
      if (!_vad.isListening) {
        await _vad.startListening(VADMode.freeTalk);
      }
      return;
    }
    _isHoldingToTalk = true;
    await _vad.startListening(VADMode.pushToTalk);
    _vad.onSpeechDetected();
  }

  Future<void> _chooseAudioInput() async {
    late final List<AudioInputDevice> devices;
    try {
      devices = await _vad.refreshInputDevices();
    } catch (_) {
      if (!mounted) return;
      _sessionController.setNotice('无法读取麦克风列表，请检查浏览器权限。');
      return;
    }
    if (!mounted) return;
    _handleAudioInputDevices(devices);
    final selected = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        top: false,
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxHeight: 420),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(
                  AppSpacing.md,
                  AppSpacing.xs,
                  AppSpacing.md,
                  AppSpacing.sm,
                ),
                child: Text(
                  '语音输入',
                  style: Theme.of(sheetContext).textTheme.titleLarge,
                ),
              ),
              Flexible(
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: devices.length,
                  itemBuilder: (_, index) {
                    final device = devices[index];
                    final selected = device.id == _selectedAudioInputId;
                    return ListTile(
                      leading: Icon(
                        selected
                            ? Icons.radio_button_checked_rounded
                            : Icons.radio_button_unchecked_rounded,
                      ),
                      title: Text(
                        device.label,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                      onTap: () => Navigator.pop(sheetContext, device.id),
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );
    if (selected == null || !mounted) return;
    final selectedDevice = devices.firstWhere(
      (device) => device.id == selected,
      orElse: () => const AudioInputDevice(
        id: '',
        label: '系统默认麦克风',
      ),
    );
    _streamingTranscription.cancelActive(_socket.send);
    await _vad.selectInputDevice(selected);
    if (_continuousEnabled && !_vad.isListening) {
      await _vad.startListening(VADMode.freeTalk);
    }
    if (!mounted) return;
    _sessionController.selectAudioInput(selected);
    _sessionController.setNotice('已切换到 ${selectedDevice.label}');
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
    _sessionController.textSubmitted(text, sent: sent);
    _runSessionCommands();
    if (shouldClearTextInput(sent)) _textController.clear();
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
      if (saved.continuousConversation) {
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

  void _schedulePresentationReturn() {
    final presentation = _activePresentation;
    if (presentation == null) return;
    _presentationReturnTimer?.cancel();
    _presentationReturnTimer = Timer(presentation.hold, () {
      if (!mounted || !identical(_activePresentation, presentation)) return;
      _sessionController.returnPresentation(presentation);
    });
  }

  void _runSessionCommands() {
    while (true) {
      final commands = _sessionController.takeCommands();
      if (commands.isEmpty) return;
      for (final command in commands) {
        switch (command) {
          case SendSocketCommand(:final message):
            if (message['type'] == 'reply_displayed') {
              WidgetsBinding.instance.addPostFrameCallback((_) {
                if (mounted) _socket.send(message);
              });
            } else {
              _socket.send(message);
            }
          case PauseAudioCommand():
            unawaited(_audio.pause());
          case PlayAudioCommand(:final audio, :final metadata):
            unawaited(_audio.playEncoded(
              audio,
              mime: metadata.mime,
              traceId: metadata.traceId,
            ));
          case StartVadCommand():
            unawaited(_vad.startListening(VADMode.freeTalk));
          case StartStreamingCaptureCommand():
            _startStreamingCapture();
          case CancelActiveTranscriptionCommand():
            _streamingTranscription.cancelActive(_socket.send);
          case SchedulePresentationReturnCommand():
            _schedulePresentationReturn();
          case CancelPresentationReturnCommand():
            _presentationReturnTimer?.cancel();
          case PersistFirstMeetingCommand():
            unawaited(_preferences.markFirstMeetingCompleted());
          case OpenTextModeCommand():
            if (mounted) setState(() => _textMode = true);
          case RequestSessionReconnectCommand():
            unawaited(_reconnectWithFreshSession());
          case SendReconnectVoiceCommand(:final fallback):
            final sent = _submitLegacyVoice(
              fallback.audio,
              queueIfOffline: false,
              signalId: fallback.signalId,
            );
            _sessionController.completeReconnectVoiceFallback(sent);
        }
      }
    }
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
    _clockTicker?.cancel();
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    _streamingTranscription.cancelAll(_socket.send);
    _textController.dispose();
    _sessionController.removeListener(_onSessionStateChanged);
    _sessionController.dispose();
    _vad.dispose();
    unawaited(_audio.dispose());
    unawaited(_socket.dispose());
    _sessions.close();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return PopScope<void>(
      canPop: _allowPop,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) unawaited(_leaveMatch(confirm: _insideMatch));
      },
      child: Scaffold(
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
                        isSpeaking: _phase == MatchSessionPhase.speaking,
                        phase: _phase,
                        socketStatus: _socketStatus,
                        continuousEnabled: _continuousEnabled,
                        subtitlesEnabled: shouldShowReplyText(
                          subtitlesEnabled: _profile.subtitlesEnabled,
                          playbackFallback: _forceSubtitleFallback,
                        ),
                        qiuqiuLine: _qiuqiuLine,
                        qiuqiuDetail: _qiuqiuDetail,
                        userLine: _userLine,
                        notice: _notice,
                        textMode: _textMode,
                        textController: _textController,
                        onToggleContinuous: _toggleContinuous,
                        audioInputLabel: _audioInputLabel,
                        onChooseAudioInput: _chooseAudioInput,
                        onOpenSettings: _openSettings,
                        onLeave: _leaveMatch,
                        onSwitchMatch: _switchMatch,
                        onReturnToCatalog: () => _leaveMatch(confirm: false),
                        onReconnect: _reconnect,
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
                        onReconnect: _reconnect,
                        onOpenSettings: _openSettings,
                        onLeave: () => _leaveMatch(confirm: false),
                      ),
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
  final VoidCallback onReconnect;
  final VoidCallback onOpenSettings;
  final VoidCallback onLeave;

  const _MatchLobby({
    super.key,
    required this.live2dKey,
    required this.match,
    required this.expression,
    required this.motion,
    required this.socketStatus,
    required this.onEnter,
    required this.onReconnect,
    required this.onOpenSettings,
    required this.onLeave,
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
                IconButton(
                  tooltip: '返回比赛列表',
                  onPressed: onLeave,
                  color: AppColors.paperInk,
                  icon: const Icon(Icons.arrow_back_rounded),
                ),
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
            if (socketStatus == SocketStatus.failed)
              OutlinedButton.icon(
                onPressed: onReconnect,
                icon: const Icon(Icons.refresh_rounded),
                label: const Text('重新连接后进入'),
              )
            else
              FilledButton(
                onPressed:
                    socketStatus == SocketStatus.connected ? onEnter : null,
                style: FilledButton.styleFrom(
                  backgroundColor: AppColors.orangeDeep,
                  foregroundColor: Colors.white,
                  side: const BorderSide(color: AppColors.paperInk, width: 2),
                  shadowColor: AppColors.paperInk,
                  elevation: 4,
                ),
                child: Text(
                  socketStatus == SocketStatus.connected
                      ? '进入球球的看台'
                      : '正在连接比赛…',
                ),
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
  final MatchSessionPhase phase;
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
  final String audioInputLabel;
  final VoidCallback onChooseAudioInput;
  final VoidCallback onOpenSettings;
  final VoidCallback onLeave;
  final VoidCallback onSwitchMatch;
  final VoidCallback onReturnToCatalog;
  final VoidCallback onReconnect;
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
    required this.audioInputLabel,
    required this.onChooseAudioInput,
    required this.onOpenSettings,
    required this.onLeave,
    required this.onSwitchMatch,
    required this.onReturnToCatalog,
    required this.onReconnect,
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
                          onLeave: onLeave,
                          onSwitchMatch: onSwitchMatch,
                          onReturnToCatalog: onReturnToCatalog,
                          onOpenSettings: onOpenSettings,
                          onReconnect: onReconnect,
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
                        audioInputLabel: audioInputLabel,
                        onChooseAudioInput: onChooseAudioInput,
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
  final VoidCallback onLeave;
  final VoidCallback onSwitchMatch;
  final VoidCallback onReturnToCatalog;
  final VoidCallback onOpenSettings;
  final VoidCallback onReconnect;

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
    required this.onLeave,
    required this.onSwitchMatch,
    required this.onReturnToCatalog,
    required this.onOpenSettings,
    required this.onReconnect,
  });

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final subtitleMaxHeight =
            (constraints.maxHeight * 0.42).clamp(128.0, 240.0).toDouble();
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
                    child: _MatchStatusCarousel(
                      items: match.statusCarouselItems,
                      contentRevision: match.statusCarouselContentRevision,
                    ),
                  ),
                  const SizedBox(width: AppSpacing.xs),
                  _ConnectionMark(status: socketStatus),
                  const SizedBox(width: AppSpacing.xs),
                  MatchActionsMenu(
                    onSelected: (action) => switch (action) {
                      MatchAction.switchMatch => onSwitchMatch(),
                      MatchAction.settings => onOpenSettings(),
                      MatchAction.leave => onLeave(),
                    },
                  ),
                ],
              ),
            ),
            if (match.liveLabel == '已结束')
              Positioned(
                top: 64,
                left: AppSpacing.md,
                child: FilledButton.icon(
                  onPressed: onReturnToCatalog,
                  icon: const Icon(Icons.list_alt_rounded),
                  label: const Text('比赛结束 · 返回比赛列表'),
                ),
              ),
            if (socketStatus == SocketStatus.failed)
              Positioned(
                top: 64,
                right: AppSpacing.md,
                child: FilledButton.icon(
                  onPressed: onReconnect,
                  icon: const Icon(Icons.refresh_rounded),
                  label: const Text('重新连接'),
                ),
              ),
            if (subtitlesEnabled)
              Positioned(
                left: AppSpacing.md,
                right: AppSpacing.sm,
                bottom: AppSpacing.md,
                child: ReplySubtitleCard(
                  primaryText: qiuqiuLine,
                  secondaryText: qiuqiuDetail,
                  maxHeight: subtitleMaxHeight,
                ),
              ),
          ],
        );
      },
    );
  }
}

class _MatchStatusCarousel extends StatefulWidget {
  final List<String> items;
  final String contentRevision;

  const _MatchStatusCarousel({
    required this.items,
    required this.contentRevision,
  });

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
    if (widget.contentRevision != oldWidget.contentRevision) {
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
  final MatchSessionPhase phase;
  final bool continuousEnabled;
  final String userLine;
  final String? notice;
  final bool textMode;
  final TextEditingController textController;
  final VoidCallback onToggleContinuous;
  final String audioInputLabel;
  final VoidCallback onChooseAudioInput;
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
    required this.audioInputLabel,
    required this.onChooseAudioInput,
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
                const SizedBox(width: AppSpacing.xs),
                Tooltip(
                  message: '选择语音输入',
                  child: InkWell(
                    onTap: onChooseAudioInput,
                    child: Padding(
                      padding: const EdgeInsets.symmetric(
                        horizontal: AppSpacing.xs,
                        vertical: AppSpacing.xxs,
                      ),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          const Icon(
                            Icons.mic_external_on_outlined,
                            size: 17,
                            color: AppColors.paperInk,
                          ),
                          const SizedBox(width: AppSpacing.xxs),
                          ConstrainedBox(
                            constraints: const BoxConstraints(maxWidth: 132),
                            child: Text(
                              audioInputLabel,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: Theme.of(context)
                                  .textTheme
                                  .labelSmall
                                  ?.copyWith(color: AppColors.paperInk),
                            ),
                          ),
                        ],
                      ),
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
      case MatchSessionPhase.welcoming:
        return AppColors.orangeDeep;
      case MatchSessionPhase.listening:
      case MatchSessionPhase.userSpeaking:
        return AppColors.green;
      case MatchSessionPhase.understanding:
        return AppColors.yellow;
      case MatchSessionPhase.speaking:
        return AppColors.orangeDeep;
      case MatchSessionPhase.permissionDenied:
      case MatchSessionPhase.offline:
      case MatchSessionPhase.failed:
        return AppColors.red;
      case MatchSessionPhase.reconnecting:
      case MatchSessionPhase.recovered:
      case MatchSessionPhase.idle:
        return AppColors.muted;
    }
  }

  String get _phaseLabel {
    switch (phase) {
      case MatchSessionPhase.welcoming:
        return '第一次见面 · 球球正在和你打招呼';
      case MatchSessionPhase.listening:
        return continuousEnabled ? '连续对话已开启 · 你直接说' : '正在听你说';
      case MatchSessionPhase.userSpeaking:
        return '听到你在说话 · 继续说';
      case MatchSessionPhase.understanding:
        return '听见了 · 正在结合比赛想一想';
      case MatchSessionPhase.speaking:
        return '球球正在回答 · 你可以随时插话';
      case MatchSessionPhase.permissionDenied:
        return '麦克风还没有权限';
      case MatchSessionPhase.offline:
        return '暂时离线 · 字幕和比赛画面仍保留';
      case MatchSessionPhase.failed:
        return '这一句没有听清';
      case MatchSessionPhase.reconnecting:
        return '重连中';
      case MatchSessionPhase.recovered:
        return '已恢复';
      case MatchSessionPhase.idle:
        return continuousEnabled ? '准备好后直接说' : '按住麦克风说话';
    }
  }
}

class _VoiceOrb extends StatelessWidget {
  final MatchSessionPhase phase;
  final bool continuousEnabled;

  const _VoiceOrb({required this.phase, required this.continuousEnabled});

  @override
  Widget build(BuildContext context) {
    final background = switch (phase) {
      MatchSessionPhase.welcoming => AppColors.orangeDeep,
      MatchSessionPhase.listening => AppColors.yellow,
      MatchSessionPhase.userSpeaking => AppColors.green,
      MatchSessionPhase.understanding => AppColors.paperInk,
      MatchSessionPhase.speaking => AppColors.orangeDeep,
      MatchSessionPhase.permissionDenied ||
      MatchSessionPhase.offline ||
      MatchSessionPhase.failed =>
        AppColors.red,
      MatchSessionPhase.reconnecting => AppColors.yellow,
      MatchSessionPhase.recovered => AppColors.green,
      MatchSessionPhase.idle => AppColors.orangeDeep,
    };
    final foreground = phase == MatchSessionPhase.listening
        ? AppColors.paperInk
        : Colors.white;
    final label = switch (phase) {
      MatchSessionPhase.welcoming => '你好呀',
      MatchSessionPhase.listening => '聆听中',
      MatchSessionPhase.userSpeaking => '你在说',
      MatchSessionPhase.understanding => '想一想',
      MatchSessionPhase.speaking => '球球在说',
      MatchSessionPhase.permissionDenied => '没权限',
      MatchSessionPhase.offline => '离线',
      MatchSessionPhase.failed => '再试一次',
      MatchSessionPhase.reconnecting => '重连中',
      MatchSessionPhase.recovered => '已恢复',
      MatchSessionPhase.idle => continuousEnabled ? '直接说' : '按住说',
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
    final failed = status == SocketStatus.failed;
    final label = connected
        ? '现场'
        : failed
            ? '连接失败'
            : '连接中';
    return Semantics(
      label: connected
          ? '比赛已连接'
          : failed
              ? '比赛连接失败'
              : '比赛正在连接',
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 8,
            height: 8,
            decoration: BoxDecoration(
              color: connected
                  ? AppColors.green
                  : failed
                      ? AppColors.red
                      : AppColors.yellow,
              shape: BoxShape.circle,
            ),
          ),
          const SizedBox(width: AppSpacing.xxs),
          Text(
            label,
            style: Theme.of(context).textTheme.labelMedium?.copyWith(
                  color: dark ? AppColors.paperInk : AppColors.ink,
                ),
          ),
        ],
      ),
    );
  }
}

bool shouldClearTextInput(bool sent) => sent;

bool shouldShowReplyText({
  required bool subtitlesEnabled,
  required bool playbackFallback,
}) {
  return subtitlesEnabled || playbackFallback;
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
