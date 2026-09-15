import 'dart:collection';

import 'package:flutter/foundation.dart';

import 'match_view_data.dart';
import 'presentation_state.dart';

enum MatchSessionPhase {
  idle,
  welcoming,
  listening,
  userSpeaking,
  understanding,
  speaking,
  permissionDenied,
  offline,
  failed,
  reconnecting,
  recovered,
}

enum MatchSessionPresence { lobby, entering, active, leaving, left }

class AudioInputViewData {
  final String id;
  final String label;

  const AudioInputViewData({required this.id, required this.label});
}

class MatchSessionState {
  final MatchSessionPresence presence;
  final MatchSessionPhase phase;
  final bool connected;
  final String transportStatus;
  final bool firstMeetingCompleted;
  final bool awaitingFirstMeetingGreeting;
  final String? activeTraceId;
  final String? notice;
  final String replyText;
  final String replyDetail;
  final String userText;
  final bool subtitleFallback;
  final MatchViewData match;
  final MatchClockViewData? matchClock;
  final String? latestFactId;
  final int latestFactRevision;
  final Map<String, int> factRevisions;
  final Set<String> retractedFactIds;
  final String expression;
  final String? motion;
  final CompanionPresentation? activePresentation;
  final int pendingAudioCount;
  final List<AudioInputViewData> audioInputDevices;
  final String selectedAudioInputId;
  final String activeAudioInputId;

  const MatchSessionState({
    this.presence = MatchSessionPresence.lobby,
    this.phase = MatchSessionPhase.idle,
    this.connected = false,
    this.transportStatus = 'connecting',
    this.firstMeetingCompleted = false,
    this.awaitingFirstMeetingGreeting = false,
    this.activeTraceId,
    this.notice,
    this.replyText = '今晚我在。开场以后，想说什么直接说。',
    this.replyDetail = '我会跟着比赛节奏回应，不打断你看球。',
    this.userText = '',
    this.subtitleFallback = false,
    this.match = const MatchViewData(),
    this.matchClock,
    this.latestFactId,
    this.latestFactRevision = 0,
    this.factRevisions = const {},
    this.retractedFactIds = const {},
    this.expression = 'idle',
    this.motion = 'idle',
    this.activePresentation,
    this.pendingAudioCount = 0,
    this.audioInputDevices = const [
      AudioInputViewData(id: '', label: '系统默认麦克风'),
    ],
    this.selectedAudioInputId = '',
    this.activeAudioInputId = '',
  });

  MatchSessionState copyWith({
    MatchSessionPresence? presence,
    MatchSessionPhase? phase,
    bool? connected,
    String? transportStatus,
    bool? firstMeetingCompleted,
    bool? awaitingFirstMeetingGreeting,
    String? activeTraceId,
    bool clearTrace = false,
    String? notice,
    bool clearNotice = false,
    String? replyText,
    String? replyDetail,
    String? userText,
    bool? subtitleFallback,
    MatchViewData? match,
    MatchClockViewData? matchClock,
    bool clearMatchClock = false,
    String? latestFactId,
    bool clearFact = false,
    int? latestFactRevision,
    Map<String, int>? factRevisions,
    Set<String>? retractedFactIds,
    String? expression,
    String? motion,
    bool clearMotion = false,
    CompanionPresentation? activePresentation,
    bool clearPresentation = false,
    int? pendingAudioCount,
    List<AudioInputViewData>? audioInputDevices,
    String? selectedAudioInputId,
    String? activeAudioInputId,
  }) {
    return MatchSessionState(
      presence: presence ?? this.presence,
      phase: phase ?? this.phase,
      connected: connected ?? this.connected,
      transportStatus: transportStatus ?? this.transportStatus,
      firstMeetingCompleted:
          firstMeetingCompleted ?? this.firstMeetingCompleted,
      awaitingFirstMeetingGreeting:
          awaitingFirstMeetingGreeting ?? this.awaitingFirstMeetingGreeting,
      activeTraceId: clearTrace ? null : (activeTraceId ?? this.activeTraceId),
      notice: clearNotice ? null : (notice ?? this.notice),
      replyText: replyText ?? this.replyText,
      replyDetail: replyDetail ?? this.replyDetail,
      userText: userText ?? this.userText,
      subtitleFallback: subtitleFallback ?? this.subtitleFallback,
      match: match ?? this.match,
      matchClock: clearMatchClock ? null : (matchClock ?? this.matchClock),
      latestFactId: clearFact ? null : (latestFactId ?? this.latestFactId),
      latestFactRevision: latestFactRevision ?? this.latestFactRevision,
      factRevisions: factRevisions ?? this.factRevisions,
      retractedFactIds: retractedFactIds ?? this.retractedFactIds,
      expression: expression ?? this.expression,
      motion: clearMotion ? null : (motion ?? this.motion),
      activePresentation: clearPresentation
          ? null
          : (activePresentation ?? this.activePresentation),
      pendingAudioCount: pendingAudioCount ?? this.pendingAudioCount,
      audioInputDevices: audioInputDevices ?? this.audioInputDevices,
      selectedAudioInputId: selectedAudioInputId ?? this.selectedAudioInputId,
      activeAudioInputId: activeAudioInputId ?? this.activeAudioInputId,
    );
  }
}

class MatchSessionController extends ChangeNotifier {
  MatchSessionState _state;
  MatchSessionPresence _presenceBeforeLeaving = MatchSessionPresence.lobby;
  final List<MatchSessionCommand> _commands = [];
  final PendingAudioQueue _pendingAudio = PendingAudioQueue();
  final DeliveryDeduplicator _seenMatchEvents = DeliveryDeduplicator();
  final DeliveryDeduplicator _seenReactions = DeliveryDeduplicator();
  final DeliveryDeduplicator _seenPresentations = DeliveryDeduplicator();
  final DeliveryDeduplicator _seenAudio = DeliveryDeduplicator();
  final Queue<ReconnectVoiceFallback> _reconnectVoiceFallbacks =
      Queue<ReconnectVoiceFallback>();
  bool _manualReconnectInProgress = false;
  String? _activeMatchReactionEventId;

  static const int maxReconnectVoiceFallbacks = 4;

  MatchSessionController({MatchViewData initialMatch = const MatchViewData()})
      : _state = MatchSessionState(match: initialMatch);

  MatchSessionState get state => _state;
  List<MatchSessionCommand> takeCommands() {
    final commands = List<MatchSessionCommand>.unmodifiable(_commands);
    _commands.clear();
    return commands;
  }

  bool get leavingActiveSession =>
      _state.presence == MatchSessionPresence.leaving &&
      _presenceBeforeLeaving == MatchSessionPresence.active;
  bool leaveRequiresConfirmation(bool requested) =>
      requested && _state.presence == MatchSessionPresence.active;
  bool get manualReconnectInProgress => _manualReconnectInProgress;

  String get audioInputLabel {
    final displayedId = _state.activeAudioInputId.isNotEmpty
        ? _state.activeAudioInputId
        : _state.selectedAudioInputId;
    for (final device in _state.audioInputDevices) {
      if (device.id == displayedId) return device.label;
    }
    return '系统默认麦克风';
  }

  bool beginEntry() {
    if (_state.presence != MatchSessionPresence.lobby) return false;
    _publish(_state.copyWith(presence: MatchSessionPresence.entering));
    return true;
  }

  MatchSessionState cancelEntry() {
    if (_state.presence == MatchSessionPresence.entering) {
      _publish(_state.copyWith(presence: MatchSessionPresence.lobby));
    }
    return _state;
  }

  MatchSessionState entered() {
    _publish(_state.copyWith(presence: MatchSessionPresence.active));
    return _state;
  }

  void openSession(String userId) {
    _commands.add(SendSocketCommand({'type': 'identify', 'userId': userId}));
    _commands
        .add(SendSocketCommand({'type': 'session_opened', 'userId': userId}));
  }

  bool beginLeaving() {
    if (_state.presence == MatchSessionPresence.leaving ||
        _state.presence == MatchSessionPresence.left) {
      return false;
    }
    _presenceBeforeLeaving = _state.presence;
    _publish(_state.copyWith(presence: MatchSessionPresence.leaving));
    return true;
  }

  MatchSessionState cancelLeaving() {
    if (_state.presence == MatchSessionPresence.leaving) {
      _publish(_state.copyWith(presence: _presenceBeforeLeaving));
    }
    return _state;
  }

  MatchSessionState left() {
    _pendingAudio.clear();
    _reconnectVoiceFallbacks.clear();
    _manualReconnectInProgress = false;
    _activeMatchReactionEventId = null;
    _publish(_state.copyWith(
      presence: MatchSessionPresence.left,
      connected: false,
      transportStatus: 'disconnected',
      phase: MatchSessionPhase.idle,
      clearTrace: true,
      awaitingFirstMeetingGreeting: false,
      pendingAudioCount: 0,
    ));
    return _state;
  }

  void _publish(MatchSessionState next) {
    _state = next;
    notifyListeners();
  }

  MatchSessionState setPhase(MatchSessionPhase phase) {
    _publish(_state.copyWith(phase: phase));
    return _state;
  }

  MatchSessionState setTransportStatus(String status) {
    _publish(_state.copyWith(
      transportStatus: status,
      connected: status == 'connected',
    ));
    return _state;
  }

  MatchSessionState initializationFailed() {
    _manualReconnectInProgress = false;
    _publish(_state.copyWith(
      connected: false,
      transportStatus: 'failed',
      phase: MatchSessionPhase.offline,
      notice: '暂时无法建立安全会话，请稍后再试。',
    ));
    return _state;
  }

  bool beginManualReconnect() {
    if (_manualReconnectInProgress) return false;
    _manualReconnectInProgress = true;
    _publish(_state.copyWith(
      connected: false,
      transportStatus: 'connecting',
      phase: MatchSessionPhase.reconnecting,
      notice: '正在重新连接比赛…',
    ));
    _commands.add(const RequestSessionReconnectCommand());
    return true;
  }

  MatchSessionState manualReconnectFailed() {
    _manualReconnectInProgress = false;
    _publish(_state.copyWith(
      connected: false,
      transportStatus: 'failed',
      phase: MatchSessionPhase.offline,
      notice: '安全会话还没准备好，请稍后再试。',
    ));
    return _state;
  }

  MatchSessionState setNotice(String? notice) {
    _publish(_state.copyWith(notice: notice, clearNotice: notice == null));
    return _state;
  }

  MatchSessionState setReply(String text, {String detail = ''}) {
    _publish(_state.copyWith(replyText: text, replyDetail: detail));
    return _state;
  }

  MatchSessionState setUserText(String text) {
    _publish(_state.copyWith(userText: text));
    return _state;
  }

  MatchSessionState setSubtitleFallback(bool enabled) {
    _publish(_state.copyWith(subtitleFallback: enabled));
    return _state;
  }

  MatchSessionState setAudioInputs(
    List<AudioInputViewData> devices, {
    required String selectedId,
    required String activeId,
  }) {
    _publish(_state.copyWith(
      audioInputDevices: List.unmodifiable(devices),
      selectedAudioInputId: selectedId,
      activeAudioInputId: activeId,
    ));
    return _state;
  }

  MatchSessionState selectAudioInput(String id) {
    _publish(_state.copyWith(selectedAudioInputId: id));
    return _state;
  }

  MatchSessionState setFirstMeetingCompleted(bool completed) {
    _publish(_state.copyWith(firstMeetingCompleted: completed));
    return _state;
  }

  MatchSessionState setExpression(String expression) {
    _publish(_state.copyWith(expression: expression));
    return _state;
  }

  MatchSessionState setMotion(String? motion) {
    _publish(_state.copyWith(motion: motion, clearMotion: motion == null));
    return _state;
  }

  MatchSessionState activatePresentation(CompanionPresentation presentation) {
    _publish(_state.copyWith(
      activePresentation: presentation,
      expression: presentation.expression,
      motion: presentation.motion,
    ));
    return _state;
  }

  MatchSessionState clearPresentation() {
    _publish(_state.copyWith(clearPresentation: true));
    return _state;
  }

  MatchSessionState returnPresentation(CompanionPresentation expected) {
    if (!identical(_state.activePresentation, expected)) return _state;
    final resting = presentationReturnState(expected);
    _publish(_state.copyWith(
      clearPresentation: true,
      expression: resting.$1,
      motion: resting.$2,
    ));
    return _state;
  }

  MatchSessionState dispatch(MatchSessionEvent event) {
    switch (event) {
      case SocketSessionEvent(:final connected, :final failed):
        return connected
            ? socketConnected()
            : socketDisconnected(failed: failed);
      case PlaybackSessionEvent(
          :final status,
          :final traceId,
          :final continuousEnabled,
          :final error,
        ):
        return handlePlayback(
          status,
          traceId: traceId,
          continuousEnabled: continuousEnabled,
          error: error,
        );
      case TranscriptSessionEvent(:final finalTranscript):
        _publish(_state.copyWith(
          phase: finalTranscript
              ? MatchSessionPhase.understanding
              : MatchSessionPhase.userSpeaking,
        ));
        return _state;
      case FirstMeetingSessionEvent():
        return markFirstMeetingCompleted();
      case MatchFactSessionEvent(
          :final factId,
          :final revision,
          :final retracted,
          :final relatedIds,
          :final continuousEnabled,
        ):
        if (factId.isEmpty) return _state;
        final currentRevision = _state.factRevisions[factId] ?? -1;
        if (revision < currentRevision) return _state;
        final revisions = Map<String, int>.of(_state.factRevisions)
          ..[factId] = revision;
        final retractedIds = Set<String>.of(_state.retractedFactIds);
        var activeReactionRetracted = false;
        if (retracted) {
          final affected =
              {factId, ...relatedIds}.where((id) => id.isNotEmpty).toSet();
          retractedIds.addAll(affected);
          for (final id in affected) {
            _pendingAudio.removeForEvent(id);
          }
          if (affected.contains(_activeMatchReactionEventId)) {
            activeReactionRetracted = true;
            _activeMatchReactionEventId = null;
            _commands.add(const PauseAudioCommand());
            _commands.add(const CancelPresentationReturnCommand());
          }
        } else {
          retractedIds.remove(factId);
        }
        final clearsLatest = retracted && factId == _state.latestFactId;
        _publish(_state.copyWith(
          latestFactId: retracted ? null : factId,
          clearFact: clearsLatest,
          latestFactRevision: clearsLatest
              ? revision
              : (retracted ? _state.latestFactRevision : revision),
          factRevisions: Map.unmodifiable(revisions),
          retractedFactIds: Set.unmodifiable(retractedIds),
          replyText: activeReactionRetracted ? _state.match.eventLabel : null,
          phase: activeReactionRetracted
              ? (continuousEnabled
                  ? MatchSessionPhase.listening
                  : MatchSessionPhase.idle)
              : null,
          clearPresentation: activeReactionRetracted,
          expression: activeReactionRetracted
              ? (continuousEnabled ? 'listening' : 'idle')
              : null,
          motion: activeReactionRetracted
              ? (continuousEnabled ? 'listen' : 'idle')
              : null,
          pendingAudioCount: _pendingAudio.length,
          clearNotice: activeReactionRetracted,
        ));
        return _state;
      case PresentationSessionEvent(:final expression, :final motion):
        _publish(_state.copyWith(expression: expression, motion: motion));
        return _state;
      case AudioQueueClearedSessionEvent():
        _pendingAudio.clear();
        _publish(_state.copyWith(pendingAudioCount: 0));
        return _state;
      case MatchSnapshotSessionEvent(:final snapshot, :final now):
        final clock = MatchClockViewData.tryParse(_map(snapshot['matchClock']));
        var match = _state.match.withSnapshot(snapshot, now: now);
        if (clock != null &&
            _state.matchClock != null &&
            clock.version < _state.matchClock!.version) {
          match = match.copyWith(
            period: _state.matchClock!.period,
            clock: _state.match.clock,
          );
          _publish(_state.copyWith(match: match));
          return _state;
        }
        _publish(_state.copyWith(match: match, matchClock: clock));
        return _state;
      case MatchClockSessionEvent(:final clock, :final now):
        final parsed = MatchClockViewData.tryParse(clock);
        if (parsed == null ||
            (_state.matchClock != null &&
                parsed.version < _state.matchClock!.version)) {
          return _state;
        }
        _publish(_state.copyWith(
          matchClock: parsed,
          match: _state.match.copyWith(
            period: parsed.period,
            clock: parsed.displayAt(now),
            hasMatchInfo: true,
          ),
        ));
        return _state;
      case MatchEventProjectionSessionEvent(
          :final event,
          :final snapshot,
          :final isNewEvent,
          :final deliveryKey,
          :final now
        ):
        final shouldProjectEvent =
            isNewEvent ?? _seenMatchEvents.remember(deliveryKey);
        final snapshotClock = snapshot == null
            ? null
            : MatchClockViewData.tryParse(_map(snapshot['matchClock']));
        var match = snapshot == null
            ? _state.match
            : _state.match.withSnapshot(snapshot, now: now);
        var matchClock = _state.matchClock;
        if (snapshotClock != null) {
          if (matchClock == null ||
              snapshotClock.version >= matchClock.version) {
            matchClock = snapshotClock;
          } else {
            match = match.copyWith(
              period: matchClock.period,
              clock: _state.match.clock,
            );
          }
        }
        if (event != null && shouldProjectEvent) match = match.withEvent(event);
        _publish(_state.copyWith(match: match, matchClock: matchClock));
        return _state;
      case LegacyMatchSessionEvent(:final score, :final minute):
        var match = _state.match;
        if (score != null) match = match.withLegacyScore(score);
        if (minute != null) match = match.copyWith(clock: "$minute'");
        _publish(_state.copyWith(match: match));
        return _state;
    }
  }

  MatchSessionState tickClock(DateTime now) {
    final clock = _state.matchClock;
    if (clock == null || !clock.running) return _state;
    final display = clock.displayAt(now);
    if (display == _state.match.clock) return _state;
    _publish(_state.copyWith(
      match: _state.match.copyWith(period: clock.period, clock: display),
    ));
    return _state;
  }

  MatchSessionState socketConnected() {
    _manualReconnectInProgress = false;
    _publish(_state.copyWith(
      connected: true,
      transportStatus: 'connected',
      phase: _state.phase == MatchSessionPhase.offline ||
              _state.phase == MatchSessionPhase.reconnecting
          ? MatchSessionPhase.recovered
          : _state.phase,
      clearNotice: true,
    ));
    _requestNextReconnectVoiceFallback();
    return _state;
  }

  MatchSessionState socketDisconnected({bool failed = false}) {
    if (failed) _manualReconnectInProgress = false;
    _publish(_state.copyWith(
      connected: false,
      transportStatus: failed ? 'failed' : 'reconnecting',
      phase:
          failed ? MatchSessionPhase.offline : MatchSessionPhase.reconnecting,
      notice: failed ? '暂时没连上比赛，但你仍可以留在这里。' : '正在回到比赛现场…',
    ));
    return _state;
  }

  MatchSessionState beginListening() {
    if (_state.phase == MatchSessionPhase.idle ||
        _state.phase == MatchSessionPhase.offline) {
      _publish(_state.copyWith(phase: MatchSessionPhase.listening));
    }
    return _state;
  }

  MatchSessionState beginSpeaking(String traceId) {
    _publish(_state.copyWith(
      phase: MatchSessionPhase.speaking,
      activeTraceId: traceId,
      clearNotice: true,
    ));
    return _state;
  }

  MatchSessionState interrupt() {
    final phase = _state.phase == MatchSessionPhase.userSpeaking ||
            _state.phase == MatchSessionPhase.understanding
        ? _state.phase
        : MatchSessionPhase.listening;
    _publish(_state.copyWith(phase: phase, clearTrace: true));
    return _state;
  }

  MatchSessionState serverInterrupted() {
    _pendingAudio.clear();
    _commands.add(const PauseAudioCommand());
    _commands.add(const CancelPresentationReturnCommand());
    final phase = _state.phase == MatchSessionPhase.userSpeaking ||
            _state.phase == MatchSessionPhase.understanding
        ? _state.phase
        : MatchSessionPhase.listening;
    _publish(_state.copyWith(
      phase: phase,
      clearTrace: true,
      clearPresentation: true,
      expression: phase == MatchSessionPhase.listening ? 'listening' : null,
      motion: phase == MatchSessionPhase.listening ? 'listen' : null,
      pendingAudioCount: 0,
    ));
    return _state;
  }

  MatchSessionState voiceStatus(String status) {
    switch (status) {
      case 'tts_fallback':
        _publish(_state.copyWith(
          phase: MatchSessionPhase.listening,
          notice: '声音暂时没出来，回答已显示在字幕里。',
        ));
      case 'failed':
        _publish(_state.copyWith(
          phase: MatchSessionPhase.failed,
          notice: '这句没听清，再说一次就好。',
        ));
    }
    return _state;
  }

  MatchSessionState markFirstMeetingCompleted() {
    _publish(_state.copyWith(firstMeetingCompleted: true));
    return _state;
  }

  MatchSessionState clearPlayback() {
    _publish(_state.copyWith(clearTrace: true));
    return _state;
  }

  void queueReconnectVoiceFallback(ReconnectVoiceFallback fallback) {
    while (_reconnectVoiceFallbacks.length >= maxReconnectVoiceFallbacks) {
      _reconnectVoiceFallbacks.removeFirst();
    }
    _reconnectVoiceFallbacks.addLast(fallback);
  }

  void completeReconnectVoiceFallback(bool sent) {
    if (!sent || _reconnectVoiceFallbacks.isEmpty) return;
    _reconnectVoiceFallbacks.removeFirst();
    _requestNextReconnectVoiceFallback();
  }

  void _requestNextReconnectVoiceFallback() {
    if (!_state.connected || _reconnectVoiceFallbacks.isEmpty) return;
    _commands.add(SendReconnectVoiceCommand(_reconnectVoiceFallbacks.first));
  }

  MatchSessionState transcriptPartial(String text,
      {bool anotherUtteranceActive = false}) {
    if (anotherUtteranceActive || text.trim().isEmpty) return _state;
    _publish(_state.copyWith(
      phase: MatchSessionPhase.userSpeaking,
      userText: text.trim(),
      clearNotice: true,
    ));
    return _state;
  }

  MatchSessionState transcriptFinal(String text,
      {bool userStillSpeaking = false}) {
    if (userStillSpeaking) return _state;
    _publish(_state.copyWith(
      phase: MatchSessionPhase.understanding,
      userText: text.trim().isEmpty ? '刚刚说的话' : text.trim(),
      expression: 'thinking',
      motion: 'think',
      clearNotice: true,
    ));
    return _state;
  }

  MatchSessionState transcriptRecoverableError(
      {bool anotherUtteranceActive = false}) {
    if (!anotherUtteranceActive) {
      _publish(_state.copyWith(notice: '实时转写暂时有点慢，正在继续识别…'));
    }
    return _state;
  }

  MatchSessionState transcriptFallbackUnavailable() {
    _publish(_state.copyWith(notice: '实时转写中断，句尾会自动重试。'));
    return _state;
  }

  MatchSessionState voiceSubmitted({
    required bool sent,
    required bool preserveCurrentCapture,
  }) {
    if (preserveCurrentCapture) {
      _publish(_state.copyWith(
        notice: sent ? null : '现在还没连上，稍后再试一次。',
        clearNotice: sent,
      ));
      return _state;
    }
    final userText =
        _state.userText.trim().isEmpty || _state.userText == '正在识别…'
            ? '刚刚说的话'
            : _state.userText;
    _publish(_state.copyWith(
      phase: sent ? MatchSessionPhase.understanding : MatchSessionPhase.offline,
      userText: userText,
      expression: sent ? 'thinking' : null,
      motion: sent ? 'think' : null,
      notice: sent ? null : '现在还没连上，稍后再试一次。',
      clearNotice: sent,
    ));
    return _state;
  }

  MatchSessionState textSubmitted(String text, {required bool sent}) {
    _commands.add(const CancelPresentationReturnCommand());
    _publish(_state.copyWith(
      userText: text,
      phase: sent ? MatchSessionPhase.understanding : MatchSessionPhase.offline,
      expression: sent ? 'thinking' : null,
      motion: sent ? 'think' : null,
      clearPresentation: sent,
      notice: sent ? null : '现在还没连上，文字没有发出去。',
      clearNotice: sent,
    ));
    return _state;
  }

  MatchSessionState vadListening({required bool hasPendingTranscript}) {
    _commands.add(
        const SendSocketCommand({'type': 'user_activity', 'state': 'idle'}));
    _commands.add(const StartStreamingCaptureCommand());
    if (hasPendingTranscript ||
        _state.phase == MatchSessionPhase.userSpeaking ||
        _state.phase == MatchSessionPhase.understanding ||
        _state.phase == MatchSessionPhase.speaking ||
        _state.phase == MatchSessionPhase.welcoming) {
      return _state;
    }
    _publish(_state.copyWith(
      phase: MatchSessionPhase.listening,
      expression: _state.activePresentation == null ? 'listening' : null,
      motion: _state.activePresentation == null ? 'listen' : null,
      clearNotice: true,
    ));
    return _state;
  }

  MatchSessionState vadSpeaking({required bool continuousEnabled}) {
    _commands.add(const SendSocketCommand(
        {'type': 'user_activity', 'state': 'speaking'}));
    if (_state.phase == MatchSessionPhase.speaking ||
        _state.awaitingFirstMeetingGreeting) {
      _commands.add(const SendSocketCommand({'type': 'interrupt'}));
      _commands.add(const PauseAudioCommand());
      _finishFirstMeetingGreeting(continuousEnabled);
    }
    _commands.add(const CancelPresentationReturnCommand());
    _publish(_state.copyWith(
      phase: MatchSessionPhase.userSpeaking,
      expression: 'focus',
      motion: 'focus',
      clearPresentation: true,
      clearTrace: true,
    ));
    return _state;
  }

  MatchSessionState vadSentenceStreamed() {
    _publish(_state.copyWith(
      phase: MatchSessionPhase.understanding,
      userText: _state.userText.trim().isEmpty ? '正在识别…' : _state.userText,
      expression: 'thinking',
      motion: 'think',
    ));
    return _state;
  }

  MatchSessionState vadIdle() {
    _commands.add(const CancelActiveTranscriptionCommand());
    _commands.add(
        const SendSocketCommand({'type': 'user_activity', 'state': 'idle'}));
    if (_state.phase != MatchSessionPhase.offline) {
      _publish(_state.copyWith(phase: MatchSessionPhase.idle));
    }
    return _state;
  }

  MatchSessionState vadPermissionDenied() {
    _commands.add(const CancelActiveTranscriptionCommand());
    _commands.add(const OpenTextModeCommand());
    _publish(_state.copyWith(
      phase: MatchSessionPhase.permissionDenied,
      notice: '没有麦克风权限，先打字也能继续陪看。',
    ));
    return _state;
  }

  MatchSessionState vadFailed() {
    _commands.add(const CancelActiveTranscriptionCommand());
    _publish(_state.copyWith(
      phase: MatchSessionPhase.failed,
      notice: '麦克风暂时没准备好，可以重试或打字。',
    ));
    return _state;
  }

  bool receiveReply({
    required String text,
    required String detail,
    String? source,
    String? eventId,
    String? deliveryKey,
    String? traceId,
    CompanionPresentation? presentation,
  }) {
    if (source == 'match_reaction' &&
        !_seenReactions.remember(deliveryKey ?? eventId)) {
      return false;
    }
    if (source == 'match_reaction' &&
        isRetractedMatchReaction(eventId, _state.retractedFactIds)) {
      return false;
    }
    final isFirstMeeting = source == 'first_meeting';
    if (source == 'match_reaction') {
      _activeMatchReactionEventId = eventId?.trim();
    } else {
      _activeMatchReactionEventId = null;
    }
    if (presentation != null) {
      _commands.add(const CancelPresentationReturnCommand());
    }
    _publish(_state.copyWith(
      subtitleFallback: false,
      replyText: text,
      replyDetail: detail,
      phase: isFirstMeeting
          ? MatchSessionPhase.welcoming
          : MatchSessionPhase.understanding,
      firstMeetingCompleted: isFirstMeeting ? true : null,
      awaitingFirstMeetingGreeting:
          isFirstMeeting ? true : _state.awaitingFirstMeetingGreeting,
      activePresentation: presentation,
      expression:
          presentation?.expression ?? (isFirstMeeting ? 'happy' : 'chat'),
      motion: presentation?.motion ?? (isFirstMeeting ? 'hello' : 'speak'),
    ));
    if (isFirstMeeting) {
      _commands.add(const PersistFirstMeetingCommand());
    }
    final normalizedTraceId = traceId?.trim() ?? '';
    if (normalizedTraceId.isNotEmpty) {
      _commands.add(SendSocketCommand(
          {'type': 'reply_displayed', 'traceId': normalizedTraceId}));
    }
    return true;
  }

  bool receivePresentation({
    required CompanionPresentation presentation,
    String? source,
    String? eventId,
    String? deliveryKey,
  }) {
    if (source == 'match_reaction' &&
        !_seenPresentations.remember(deliveryKey)) {
      return false;
    }
    if (source == 'match_reaction' &&
        isRetractedMatchReaction(eventId, _state.retractedFactIds)) {
      return false;
    }
    if (source == 'match_reaction') {
      _activeMatchReactionEventId = eventId?.trim();
    }
    _commands.add(const CancelPresentationReturnCommand());
    _publish(_state.copyWith(
      activePresentation: presentation,
      expression: presentation.expression,
      motion: presentation.motion,
    ));
    _commands.add(const SchedulePresentationReturnCommand());
    return true;
  }

  MatchSessionState receiveLegacyExpression(String expression, String motion) {
    _commands.add(const CancelPresentationReturnCommand());
    _publish(_state.copyWith(
      clearPresentation: true,
      expression: expression,
      motion: motion,
    ));
    return _state;
  }

  MatchSessionState receiveLegacyEventAnimation(
      String expression, String motion) {
    _publish(_state.copyWith(expression: expression, motion: motion));
    return _state;
  }

  void queueAudioMetadata(PendingAudio metadata, {String? source}) {
    final duplicate = source == 'match_reaction' &&
        !_seenAudio.remember(metadata.deliveryKey);
    final retracted = source == 'match_reaction' &&
        isRetractedMatchReaction(metadata.eventId, _state.retractedFactIds);
    _pendingAudio.add(metadata.copyWith(skip: duplicate || retracted));
    _publish(_state.copyWith(pendingAudioCount: _pendingAudio.length));
  }

  void consumeAudio(
    Uint8List audioBytes, {
    required bool soundEnabled,
    required bool continuousEnabled,
  }) {
    final metadata =
        _pendingAudio.take() ?? const PendingAudio(mime: 'audio/wav');
    if (metadata.skip) {
      final receipt = mutedPlaybackReceipt(metadata);
      if (receipt != null) _commands.add(SendSocketCommand(receipt));
      _publish(_state.copyWith(pendingAudioCount: _pendingAudio.length));
      return;
    }
    if (!soundEnabled) {
      final receipt = mutedPlaybackReceipt(metadata);
      if (receipt != null) _commands.add(SendSocketCommand(receipt));
      _publish(_state.copyWith(
        pendingAudioCount: _pendingAudio.length,
        phase: continuousEnabled
            ? MatchSessionPhase.listening
            : MatchSessionPhase.idle,
      ));
      _commands.add(const SchedulePresentationReturnCommand());
      _finishFirstMeetingGreeting(continuousEnabled);
      return;
    }
    _commands.add(PlayAudioCommand(audioBytes, metadata));
    _publish(_state.copyWith(pendingAudioCount: _pendingAudio.length));
  }

  MatchSessionState handlePlayback(
    String status, {
    String? traceId,
    required bool continuousEnabled,
    String? error,
  }) {
    final normalizedTraceId = traceId?.trim() ?? '';
    if (normalizedTraceId.isNotEmpty) {
      _commands.add(SendSocketCommand({
        'type': 'voice_playback',
        'traceId': normalizedTraceId,
        'state': status == 'failed' ? 'error' : status,
      }));
    }
    switch (status) {
      case 'started':
        _commands.add(const CancelPresentationReturnCommand());
        _publish(_state.copyWith(
          phase: MatchSessionPhase.speaking,
          activeTraceId: normalizedTraceId,
          expression: _state.activePresentation == null ? 'chat' : null,
          motion: _state.activePresentation == null ? 'speak' : null,
          clearNotice: true,
        ));
      case 'ended':
      case 'completed':
        _publish(_state.copyWith(
          phase: continuousEnabled
              ? MatchSessionPhase.listening
              : MatchSessionPhase.idle,
          clearTrace: true,
          expression: _state.activePresentation == null
              ? (continuousEnabled ? 'listening' : 'idle')
              : null,
          motion: _state.activePresentation == null
              ? (continuousEnabled ? 'listen' : 'idle')
              : null,
          clearNotice: true,
        ));
        _commands.add(const SchedulePresentationReturnCommand());
        _finishFirstMeetingGreeting(continuousEnabled);
      case 'interrupted':
        final preserved = _state.phase == MatchSessionPhase.userSpeaking ||
                _state.phase == MatchSessionPhase.understanding
            ? _state.phase
            : MatchSessionPhase.listening;
        _commands.add(const CancelPresentationReturnCommand());
        _publish(_state.copyWith(
          phase: preserved,
          clearTrace: true,
          clearPresentation: true,
          expression:
              preserved == MatchSessionPhase.listening ? 'listening' : null,
          motion: preserved == MatchSessionPhase.listening ? 'listen' : null,
        ));
      case 'blocked':
        _publish(_state.copyWith(
          subtitleFallback: true,
          phase: continuousEnabled
              ? MatchSessionPhase.listening
              : MatchSessionPhase.idle,
          notice: '轻触一下屏幕，我就能开口。',
          clearTrace: true,
        ));
        _commands.add(const SchedulePresentationReturnCommand());
        _finishFirstMeetingGreeting(continuousEnabled);
      case 'failed':
        _publish(_state.copyWith(
          subtitleFallback: true,
          phase: continuousEnabled
              ? MatchSessionPhase.listening
              : MatchSessionPhase.idle,
          notice: '声音暂时没播放出来，字幕还在。',
          clearTrace: true,
        ));
        _commands.add(const SchedulePresentationReturnCommand());
        _finishFirstMeetingGreeting(continuousEnabled);
    }
    return _state;
  }

  MatchSessionState handleVoiceStatus(String status,
      {required bool continuousEnabled}) {
    switch (status) {
      case 'text_fallback':
        _publish(_state.copyWith(notice: '这句没听清，已经用文字继续。'));
      case 'tts_fallback':
        _publish(_state.copyWith(
          notice: '声音暂时没出来，回答已显示在字幕里。',
          subtitleFallback: true,
          phase: continuousEnabled
              ? MatchSessionPhase.listening
              : MatchSessionPhase.idle,
        ));
        _commands.add(const SchedulePresentationReturnCommand());
        _finishFirstMeetingGreeting(continuousEnabled);
      case 'failed':
        _publish(_state.copyWith(
          notice: '这句没听清，再说一次就好。',
          phase: MatchSessionPhase.failed,
        ));
        _commands.add(const SchedulePresentationReturnCommand());
        _finishFirstMeetingGreeting(continuousEnabled);
    }
    return _state;
  }

  MatchSessionState firstMeetingRequested(
      {required bool sent, required bool continuousEnabled}) {
    if (!sent) {
      if (continuousEnabled) _commands.add(const StartVadCommand());
      return _state;
    }
    _publish(_state.copyWith(
      awaitingFirstMeetingGreeting: true,
      phase: MatchSessionPhase.welcoming,
      expression: 'happy',
      motion: 'hello',
      replyText: '嗨，我是球球。',
      replyDetail: '第一次见面，先让我认真和你打个招呼。',
      clearNotice: true,
    ));
    if (continuousEnabled) _commands.add(const StartVadCommand());
    return _state;
  }

  bool firstMeetingStatus(String status, {required bool continuousEnabled}) {
    if (status != 'delivered' && status != 'skipped') return false;
    if (!_state.firstMeetingCompleted) {
      _publish(_state.copyWith(firstMeetingCompleted: true));
      _commands.add(const PersistFirstMeetingCommand());
    }
    if (status == 'skipped') {
      _finishFirstMeetingGreeting(continuousEnabled);
    }
    return true;
  }

  void finishFirstMeetingGreeting({required bool continuousEnabled}) {
    _finishFirstMeetingGreeting(continuousEnabled);
  }

  void _finishFirstMeetingGreeting(bool continuousEnabled) {
    if (!_state.awaitingFirstMeetingGreeting) return;
    _publish(_state.copyWith(awaitingFirstMeetingGreeting: false));
    if (_state.presence == MatchSessionPresence.active && continuousEnabled) {
      _commands.add(const StartVadCommand());
    }
  }
}

sealed class MatchSessionEvent {
  const MatchSessionEvent();
}

sealed class MatchSessionCommand {
  const MatchSessionCommand();
}

class SendSocketCommand extends MatchSessionCommand {
  final Map<String, dynamic> message;
  const SendSocketCommand(this.message);
}

class PauseAudioCommand extends MatchSessionCommand {
  const PauseAudioCommand();
}

class PlayAudioCommand extends MatchSessionCommand {
  final Uint8List audio;
  final PendingAudio metadata;
  PlayAudioCommand(Uint8List audio, this.metadata)
      : audio = Uint8List.fromList(audio);
}

class StartVadCommand extends MatchSessionCommand {
  const StartVadCommand();
}

class StartStreamingCaptureCommand extends MatchSessionCommand {
  const StartStreamingCaptureCommand();
}

class CancelActiveTranscriptionCommand extends MatchSessionCommand {
  const CancelActiveTranscriptionCommand();
}

class SchedulePresentationReturnCommand extends MatchSessionCommand {
  const SchedulePresentationReturnCommand();
}

class CancelPresentationReturnCommand extends MatchSessionCommand {
  const CancelPresentationReturnCommand();
}

class PersistFirstMeetingCommand extends MatchSessionCommand {
  const PersistFirstMeetingCommand();
}

class OpenTextModeCommand extends MatchSessionCommand {
  const OpenTextModeCommand();
}

class RequestSessionReconnectCommand extends MatchSessionCommand {
  const RequestSessionReconnectCommand();
}

class SendReconnectVoiceCommand extends MatchSessionCommand {
  final ReconnectVoiceFallback fallback;
  const SendReconnectVoiceCommand(this.fallback);
}

class SocketSessionEvent extends MatchSessionEvent {
  final bool connected;
  final bool failed;
  const SocketSessionEvent({required this.connected, this.failed = false});
}

class PlaybackSessionEvent extends MatchSessionEvent {
  final String status;
  final String? traceId;
  final bool continuousEnabled;
  final String? error;
  const PlaybackSessionEvent(
    this.status, {
    this.traceId,
    this.continuousEnabled = false,
    this.error,
  });
}

class TranscriptSessionEvent extends MatchSessionEvent {
  final bool finalTranscript;
  const TranscriptSessionEvent({required this.finalTranscript});
}

class FirstMeetingSessionEvent extends MatchSessionEvent {
  const FirstMeetingSessionEvent();
}

class MatchFactSessionEvent extends MatchSessionEvent {
  final String factId;
  final int revision;
  final bool retracted;
  final Set<String> relatedIds;
  final bool continuousEnabled;
  const MatchFactSessionEvent(this.factId, this.revision,
      {this.retracted = false,
      this.relatedIds = const {},
      this.continuousEnabled = false});
}

class PresentationSessionEvent extends MatchSessionEvent {
  final String expression;
  final String motion;
  const PresentationSessionEvent(
      {required this.expression, required this.motion});
}

class AudioQueueClearedSessionEvent extends MatchSessionEvent {
  const AudioQueueClearedSessionEvent();
}

class MatchSnapshotSessionEvent extends MatchSessionEvent {
  final Map<String, dynamic> snapshot;
  final DateTime now;
  MatchSnapshotSessionEvent(Map<String, dynamic> snapshot, this.now)
      : snapshot = Map.unmodifiable(snapshot);
}

class MatchClockSessionEvent extends MatchSessionEvent {
  final Map<String, dynamic> clock;
  final DateTime now;
  MatchClockSessionEvent(Map<String, dynamic> clock, this.now)
      : clock = Map.unmodifiable(clock);
}

class MatchEventProjectionSessionEvent extends MatchSessionEvent {
  final Map<String, dynamic>? event;
  final Map<String, dynamic>? snapshot;
  final bool? isNewEvent;
  final String? deliveryKey;
  final DateTime now;

  MatchEventProjectionSessionEvent({
    Map<String, dynamic>? event,
    Map<String, dynamic>? snapshot,
    this.isNewEvent,
    this.deliveryKey,
    required this.now,
  })  : event = event == null ? null : Map.unmodifiable(event),
        snapshot = snapshot == null ? null : Map.unmodifiable(snapshot);
}

class LegacyMatchSessionEvent extends MatchSessionEvent {
  final String? score;
  final Object? minute;
  const LegacyMatchSessionEvent({this.score, this.minute});
}

class PendingAudio {
  final String mime;
  final String? traceId;
  final String? eventId;
  final String? deliveryKey;
  final int? byteLength;
  final bool skip;

  const PendingAudio({
    required this.mime,
    this.traceId,
    this.eventId,
    this.deliveryKey,
    this.byteLength,
    this.skip = false,
  });

  PendingAudio copyWith({bool? skip}) => PendingAudio(
        mime: mime,
        traceId: traceId,
        eventId: eventId,
        deliveryKey: deliveryKey,
        byteLength: byteLength,
        skip: skip ?? this.skip,
      );
}

class PendingAudioQueue {
  final Queue<PendingAudio> _items = Queue<PendingAudio>();

  int get length => _items.length;
  void add(PendingAudio metadata) => _items.addLast(metadata);
  PendingAudio? take() => _items.isEmpty ? null : _items.removeFirst();

  void removeForEvent(String eventId) {
    final normalized = eventId.trim();
    if (normalized.isEmpty) return;
    _items.removeWhere((item) => item.eventId?.trim() == normalized);
  }

  void clear() => _items.clear();
}

class DeliveryDeduplicator {
  final int capacity;
  final LinkedHashSet<String> _seen = LinkedHashSet<String>();

  DeliveryDeduplicator({this.capacity = 256});

  bool remember(String? deliveryKey) {
    final normalized = deliveryKey?.trim() ?? '';
    if (normalized.isEmpty) return true;
    if (!_seen.add(normalized)) return false;
    if (_seen.length > capacity) _seen.remove(_seen.first);
    return true;
  }
}

class ReconnectVoiceFallback {
  final Uint8List audio;
  final String? signalId;

  ReconnectVoiceFallback({required Uint8List audio, this.signalId})
      : audio = Uint8List.fromList(audio);
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

bool isRetractedMatchReaction(String? eventId, Set<String> retractedEventIds) {
  final normalized = eventId?.trim() ?? '';
  return normalized.isNotEmpty && retractedEventIds.contains(normalized);
}

String? matchEventDeliveryKey(Map<String, dynamic>? event) {
  final eventId = event?['id']?.toString().trim() ?? '';
  if (eventId.isEmpty) return null;
  final revision = event?['factRevision']?.toString() ?? '0';
  final status = event?['factStatus']?.toString() ?? '';
  return '$eventId:$revision:$status';
}

Map<String, dynamic>? _map(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return value.cast<String, dynamic>();
  return null;
}
