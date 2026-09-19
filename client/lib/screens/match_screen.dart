import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:flutter/foundation.dart' show debugPrint, kIsWeb;
import 'package:flutter/material.dart';
import 'package:lottie/lottie.dart';
import 'package:vibration/vibration.dart';

export '../services/match_view_data.dart';

import '../services/audio_player.dart';
import '../services/idle_tier_picker.dart';
import '../services/preferences_service.dart';
import '../services/recorder_stub.dart';
import '../services/session_service.dart';
import '../services/match_session_controller.dart';
import '../services/match_view_data.dart';
import '../services/match_overview_service.dart';
import '../services/portrait_service.dart';
import '../services/streaming_transcription.dart';
import '../services/websocket_service.dart';
import '../theme/app_theme.dart';
import '../widgets/live2d_view.dart';
import '../widgets/match_actions_menu.dart';
import '../widgets/mobile_theme_canvas.dart';
import '../widgets/reply_subtitle_card.dart';
import 'match_dock.dart';
import '../widgets/connection_mark.dart';
import 'match_lobby.dart';
import 'portrait_screen.dart';
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
  final MatchOverviewService _overviewService = MatchOverviewService();
  late final MatchSessionController _sessionController;
  final VADService _vad = VADService();
  final StreamingTranscription _streamingTranscription =
      StreamingTranscription();
  final TextEditingController _textController = TextEditingController();
  final GlobalKey<Live2dViewState> _live2dKey = GlobalKey<Live2dViewState>();
  final List<StreamSubscription<dynamic>> _subscriptions = [];
  final IdleTierPicker _idlePicker = IdleTierPicker();
  late final Future<void> _profileLoad;
  MatchOverviewData _overview = const MatchOverviewData();
  bool _overviewLoading = true;
  String? _overviewError;

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
  Timer? _idleTicker;
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
    // 表演映射单一源（ADR-0007）：相位行从 presentation-map.json 解析。
    unawaited(_sessionController.ensurePresentationMapLoaded());
    _bindServices();
    unawaited(_loadMatchOverview());
    _clockTicker = Timer.periodic(const Duration(milliseconds: 250), (_) {
      if (!mounted) return;
      _sessionController.tickClock(DateTime.now().toUtc());
    });
    _idleTicker = Timer.periodic(const Duration(seconds: 30), (_) {
      _repickIdleMotion();
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

  String _overviewMatchId() {
    final requested = widget.matchId?.trim();
    if (requested != null && requested.isNotEmpty) return requested;
    if (kIsWeb) {
      final fromPage = Uri.base.queryParameters['matchId']?.trim();
      if (fromPage != null && fromPage.isNotEmpty) return fromPage;
    }
    return 'test';
  }

  Future<void> _loadMatchOverview() async {
    try {
      final overview = await _overviewService.fetch(
        baseUrl: normalizeAPIBaseURL(_socketUrl()),
        matchId: _overviewMatchId(),
      );
      if (!mounted) return;
      setState(() {
        _overview = overview;
        _overviewLoading = false;
        _overviewError = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _overviewLoading = false;
        _overviewError = error.toString();
      });
    }
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
    } on SessionException catch (error) {
      if (error.statusCode != 409) {
        if (!mounted) return;
        _sessionController.initializationFailed();
        return;
      }
      // 后端 409 ErrIdentityUnavailable：隐私删除/映射过期——如实重置后
      // 以全新匿名身份开始一次（不重试旧身份、不降级）。
      final token = await _restartAsNewIdentity();
      if (!mounted) return;
      if (token == null) {
        _sessionController.initializationFailed();
        return;
      }
      _socket.connect(_socketUrl(), token: token);
    } catch (_) {
      if (!mounted) return;
      _sessionController.initializationFailed();
    }
  }

  /// 409 ErrIdentityUnavailable（隐私删除/映射过期）：清除本地 deviceId，
  /// 生成全新匿名身份后重建一次会话；再次失败则交回初始化失败路径。
  Future<String?> _restartAsNewIdentity() async {
    await _preferences.resetAnonymousIdentity();
    final freshDeviceId = await _preferences.loadOrCreateAnonymousUserId();
    if (!mounted) return null;
    setState(() {
      _deviceId = freshDeviceId;
    });
    try {
      final session = await _sessions.ensureSession(
        baseUrl: normalizeAPIBaseURL(_socketUrl()),
        deviceId: freshDeviceId,
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
    } on SessionException catch (error) {
      if (error.statusCode != 409) return null;
      return _restartAsNewIdentity();
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
          _overview = _overview.withSnapshot(snapshot);
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
        if (snapshot != null) _overview = _overview.withSnapshot(snapshot);
        final deliveryKey =
            message['deliveryKey']?.toString() ?? matchEventDeliveryKey(event);
        _sessionController.dispatch(MatchEventProjectionSessionEvent(
          event: event,
          snapshot: snapshot,
          deliveryKey: deliveryKey,
          now: DateTime.now().toUtc(),
        ));
        if (event != null) {
          _vibrateForMatchEvent(event['eventType']?.toString());
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
          _idlePicker.updateAffect(
            valence: presentation.valence,
            arousal: presentation.arousal,
          );
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
      final reply = data?['text'] as String? ?? '';
      final source = data?['source']?.toString();
      final eventId = data?['eventId']?.toString();
      final traceId = data?['traceId'] as String?;
      final presentation = CompanionPresentation.fromReplyData(data);
      final trimmed = reply.trim();
      if (trimmed.isEmpty && presentation == null) return;
      if (presentation != null) {
        _idlePicker.updateAffect(
          valence: presentation.valence,
          arousal: presentation.arousal,
        );
      }
      if (trimmed.isEmpty) {
        // 会话开场 hello（后端 delivery.go 的 Text:"" + presentation）：
        // 只上表演并走保持期，不进对话流。
        _sessionController.receivePresentation(
          // 上方已保证二者不同时为空，此处非空。
          presentation: presentation!,
          source: source,
          eventId: eventId,
          deliveryKey: data?['deliveryKey']?.toString(),
        );
        _runSessionCommands();
        return;
      }
      final parts = splitReplyForDisplay(trimmed);
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
    _vibrateForMatchEvent(eventType);
    if (eventType == 'match_end') {
      // 全场结束走相位表 match_end 行（一次性 happy/wave 告别）。
      _sessionController.applyMatchEnd();
    }
  }

  void _vibrateForMatchEvent(String? eventType) {
    if (!kIsWeb &&
        (eventType == 'goal' ||
            eventType == 'penalty' ||
            eventType == 'red_card')) {
      unawaited(Vibration.vibrate(duration: 90));
    }
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
    // Lip sync follows the platform audio playback lifecycle.
    final live2d = _live2dKey.currentState;
    if (live2d != null) {
      if (state.status == AudioPlaybackStatus.started) {
        live2d.startLipSync();
      } else {
        live2d.stopLipSync();
      }
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
        builder: (_) => SettingsScreen(
          initialProfile: _profile,
          onSave: _preferences.save,
          onOpenPortrait: _deviceId.isEmpty ? null : _openPortrait,
        ),
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

  /// 球球懂我 (C3): the portrait page talks to /api/me/portrait over the same
  /// session the match transport uses, so it works with or without a live
  /// match connection.
  Future<void> _openPortrait() async {
    await Navigator.push(
      context,
      MaterialPageRoute(
        builder: (_) => PortraitScreen(
          service: PortraitService(
            baseUrl: normalizeAPIBaseURL(_socketUrl()),
            sessions: _sessions,
            deviceId: _deviceId,
          ),
        ),
      ),
    );
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

  /// Quiet-stretch idle re-pick: maps the last received affect vector onto a
  /// tier and replays the tier's idle motion (hysteresis in IdleTierPicker).
  void _repickIdleMotion() {
    if (!mounted) return;
    final state = _sessionController.state;
    if (state.activePresentation != null ||
        state.phase != MatchSessionPhase.idle) {
      return;
    }
    final motion = _idlePicker.maybeRepick(DateTime.now().toUtc());
    if (motion == null || motion == state.motion) return;
    _sessionController.setMotion(motion);
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
            _live2dKey.currentState?.queueLipSyncAudio(
              audio,
              mime: metadata.mime,
            );
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

  @override
  void dispose() {
    _presentationReturnTimer?.cancel();
    _idleTicker?.cancel();
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
    _overviewService.close();
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
        backgroundColor: AppColors.night,
        body: MobileThemeCanvas(
          backgroundAsset: 'assets/images/stadium-stage.png',
          overlayColor:
              _insideMatch ? const Color(0x0D070B12) : const Color(0x1F0B2E68),
          child: SafeArea(
            child: LayoutBuilder(
              builder: (context, constraints) {
                final content = _insideMatch
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
                    : MatchLobby(
                        key: const ValueKey('lobby'),
                        match: _match,
                        overview: _overview,
                        overviewLoading: _overviewLoading,
                        overviewError: _overviewError,
                        socketStatus: _socketStatus,
                        onEnter: _enterMatch,
                        onReconnect: _reconnect,
                        onOpenSettings: _openSettings,
                        onLeave: () => _leaveMatch(confirm: false),
                      );
                final isCompact = constraints.maxWidth <= 720;
                return Align(
                  alignment: Alignment.topCenter,
                  child: isCompact
                      ? SizedBox.expand(child: content)
                      : ConstrainedBox(
                          constraints: const BoxConstraints(maxWidth: 520),
                          child: content,
                        ),
                );
              },
            ),
          ),
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
    return Stack(
      fit: StackFit.expand,
      children: [
        Column(
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
                        ConversationDock(
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
      ],
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
        color: const Color(0xB30B2E68),
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
      color: const Color(0xB30B2E68),
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
            Live2dView(
              key: live2dKey,
              expression: expression,
              isSpeaking: isSpeaking,
              motion: motion,
            ),
            // 进球爆屏：按事件 ID 边沿触发（每个新进球播一次，重建不重放）。
            Positioned.fill(
              child: IgnorePointer(
                child: _GoalBurstOverlay(latestGoalEventId: match.latestGoalEventId),
              ),
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
                  ConnectionMark(status: socketStatus),
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

/// 进球爆屏（ADR-0007 归属修正）：进球是比赛事件可视化，按事件 ID 边沿
/// 触发——每个新进球只播一次，widget 重建不重放；不再按展示标签字符串
/// 判断内容。
class _GoalBurstOverlay extends StatefulWidget {
  final String latestGoalEventId;

  const _GoalBurstOverlay({required this.latestGoalEventId});

  @override
  State<_GoalBurstOverlay> createState() => _GoalBurstOverlayState();
}

class _GoalBurstOverlayState extends State<_GoalBurstOverlay> {
  bool _visible = false;
  Timer? _hideTimer;

  @override
  void didUpdateWidget(covariant _GoalBurstOverlay oldWidget) {
    super.didUpdateWidget(oldWidget);
    final next = widget.latestGoalEventId;
    if (next.isNotEmpty && next != oldWidget.latestGoalEventId) {
      _hideTimer?.cancel();
      setState(() => _visible = true);
      _hideTimer = Timer(const Duration(milliseconds: 2200), () {
        if (mounted) setState(() => _visible = false);
      });
    }
  }

  @override
  void dispose() {
    _hideTimer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!_visible) return const SizedBox.shrink();
    return Center(
      child: SizedBox(
        width: 180,
        height: 180,
        child: Lottie.asset(
          'assets/animations/goal-burst.json',
          repeat: false,
        ),
      ),
    );
  }
}
