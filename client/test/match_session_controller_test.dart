import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/match_session_controller.dart';
import 'package:qiuqiu/services/presentation_state.dart';

void main() {
  test('owns the complete match presence lifecycle', () {
    final controller = MatchSessionController();
    expect(controller.beginEntry(), isTrue);
    expect(controller.leaveRequiresConfirmation(true), isFalse);
    expect(controller.state.presence, MatchSessionPresence.entering);
    controller.entered();
    expect(controller.leaveRequiresConfirmation(true), isTrue);
    expect(controller.leaveRequiresConfirmation(false), isFalse);
    expect(controller.state.presence, MatchSessionPresence.active);
    expect(controller.beginLeaving(), isTrue);
    expect(controller.leavingActiveSession, isTrue);
    controller.cancelLeaving();
    expect(controller.state.presence, MatchSessionPresence.active);
    expect(controller.beginLeaving(), isTrue);
    controller.left();
    expect(controller.state.presence, MatchSessionPresence.left);
    expect(controller.state.connected, isFalse);
    expect(controller.state.pendingAudioCount, 0);
    expect(controller.beginLeaving(), isFalse);
  });

  test('recovers a logical session after transport reconnect', () {
    final controller = MatchSessionController();
    controller.socketDisconnected(failed: true);
    expect(controller.state.phase, MatchSessionPhase.offline);
    controller.socketConnected();
    expect(controller.state.connected, isTrue);
    expect(controller.state.phase, MatchSessionPhase.recovered);
    expect(controller.state.transportStatus, 'connected');
  });

  test('moves through speaking interrupt reconnect and recovery', () {
    final controller = MatchSessionController();
    controller.beginListening();
    controller.beginSpeaking('trace-3');
    expect(controller.state.phase, MatchSessionPhase.speaking);
    controller.interrupt();
    expect(controller.state.phase, MatchSessionPhase.listening);
    controller.socketDisconnected();
    expect(controller.state.phase, MatchSessionPhase.reconnecting);
    controller.socketConnected();
    expect(controller.state.phase, MatchSessionPhase.recovered);
  });

  test('interrupt preserves active user turn but stops playback', () {
    final controller = MatchSessionController();
    controller.beginSpeaking('trace-1');
    expect(controller.state.activeTraceId, 'trace-1');
    controller.interrupt();
    expect(controller.state.phase, MatchSessionPhase.listening);
    expect(controller.state.activeTraceId, isNull);
  });

  test('voice fallback returns to listening with explicit notice', () {
    final controller = MatchSessionController();
    controller.beginSpeaking('trace-2');
    controller.voiceStatus('tts_fallback');
    expect(controller.state.phase, MatchSessionPhase.listening);
    expect(controller.state.notice, isNotEmpty);
  });

  test('dispatch normalizes socket, transcript and playback events', () {
    final controller = MatchSessionController();
    controller.dispatch(const SocketSessionEvent(connected: true));
    controller.dispatch(const TranscriptSessionEvent(finalTranscript: false));
    expect(controller.state.phase, MatchSessionPhase.userSpeaking);
    controller.dispatch(const TranscriptSessionEvent(finalTranscript: true));
    expect(controller.state.phase, MatchSessionPhase.understanding);
    controller.dispatch(const PlaybackSessionEvent('started', traceId: 't-1'));
    expect(controller.state.phase, MatchSessionPhase.speaking);
    controller.dispatch(const FirstMeetingSessionEvent());
    expect(controller.state.firstMeetingCompleted, isTrue);
  });

  test('fact retraction clears the currently projected fact', () {
    final controller = MatchSessionController();
    controller.dispatch(const MatchFactSessionEvent('fact-1', 2));
    expect(controller.state.latestFactId, 'fact-1');
    controller.dispatch(
      const MatchFactSessionEvent('fact-1', 3, retracted: true),
    );
    expect(controller.state.latestFactId, isNull);
    expect(controller.state.latestFactRevision, 3);
  });

  test('stale fact retractions do not replace newer projections', () {
    final controller = MatchSessionController();
    controller.dispatch(const MatchFactSessionEvent('fact-1', 4));
    controller.dispatch(
      const MatchFactSessionEvent('fact-old', 3, retracted: true),
    );
    expect(controller.state.latestFactId, 'fact-1');
    expect(controller.state.latestFactRevision, 4);
  });

  test('fact revisions are compared per fact instead of globally', () {
    final controller = MatchSessionController();
    controller.dispatch(const MatchFactSessionEvent('fact-1', 4));
    controller.dispatch(const MatchFactSessionEvent('fact-2', 1));
    expect(controller.state.latestFactId, 'fact-2');
    expect(controller.state.latestFactRevision, 1);
    expect(controller.state.factRevisions, {'fact-1': 4, 'fact-2': 1});
  });

  test('fact retractions retain related event ids', () {
    final controller = MatchSessionController();
    controller.queueAudioMetadata(const PendingAudio(
      mime: 'audio/mpeg',
      traceId: 'trace-1',
      eventId: 'event-1',
    ));
    controller.dispatch(const MatchFactSessionEvent(
      'fact-1',
      2,
      retracted: true,
      relatedIds: {'event-1'},
    ));
    expect(
        controller.state.retractedFactIds, containsAll({'fact-1', 'event-1'}));
    expect(controller.state.pendingAudioCount, 0);
  });

  test('clearing the audio queue resets its projection', () {
    final controller = MatchSessionController();
    controller.queueAudioMetadata(
        const PendingAudio(mime: 'audio/mpeg', traceId: 'trace-1'));
    controller.queueAudioMetadata(
        const PendingAudio(mime: 'audio/mpeg', traceId: 'trace-2'));
    controller.dispatch(const AudioQueueClearedSessionEvent());
    expect(controller.state.pendingAudioCount, 0);
    expect(controller.takeCommands(), isEmpty);
  });

  test('owns reconnect locking and queued voice retry ordering', () {
    final controller = MatchSessionController();
    expect(controller.beginManualReconnect(), isTrue);
    expect(controller.beginManualReconnect(), isFalse);
    expect(controller.state.phase, MatchSessionPhase.reconnecting);
    expect(controller.takeCommands().single,
        isA<RequestSessionReconnectCommand>());

    controller.queueReconnectVoiceFallback(ReconnectVoiceFallback(
      audio: Uint8List.fromList([1]),
      signalId: 'signal-1',
    ));
    controller.queueReconnectVoiceFallback(ReconnectVoiceFallback(
      audio: Uint8List.fromList([2]),
      signalId: 'signal-2',
    ));
    controller.socketConnected();
    var retry = controller.takeCommands().single as SendReconnectVoiceCommand;
    expect(retry.fallback.signalId, 'signal-1');
    controller.completeReconnectVoiceFallback(true);
    retry = controller.takeCommands().single as SendReconnectVoiceCommand;
    expect(retry.fallback.signalId, 'signal-2');
  });

  test('normalizes VAD input into state and platform commands', () {
    final controller = MatchSessionController();
    controller.vadListening(hasPendingTranscript: false);
    expect(controller.state.phase, MatchSessionPhase.listening);
    expect(controller.takeCommands(), [
      isA<SendSocketCommand>(),
      isA<StartStreamingCaptureCommand>(),
    ]);

    controller.vadSpeaking(continuousEnabled: true);
    expect(controller.state.phase, MatchSessionPhase.userSpeaking);
    expect(controller.state.expression, 'focus');
  });

  test('deduplicates event projections and consumes duplicate audio safely',
      () {
    final controller = MatchSessionController();
    final now = DateTime.utc(2026, 9, 8, 20);
    for (var index = 0; index < 2; index++) {
      controller.dispatch(MatchEventProjectionSessionEvent(
        event: const {
          'eventType': 'goal',
          'clock': "24'",
          'playerName': '萨拉赫',
        },
        deliveryKey: 'goal-1:1:confirmed',
        now: now,
      ));
    }
    expect(controller.state.match.recentEventLabels, hasLength(1));

    for (var index = 0; index < 2; index++) {
      controller.queueAudioMetadata(
          const PendingAudio(
            mime: 'audio/mpeg',
            traceId: 'trace-goal',
            eventId: 'goal-1',
            deliveryKey: 'goal-1:1:confirmed',
          ),
          source: 'match_reaction');
    }
    controller.consumeAudio(Uint8List.fromList([1]),
        soundEnabled: true, continuousEnabled: true);
    controller.consumeAudio(Uint8List.fromList([2]),
        soundEnabled: true, continuousEnabled: true);
    final commands = controller.takeCommands();
    expect(commands.whereType<PlayAudioCommand>(), hasLength(1));
    expect(
      commands
          .whereType<SendSocketCommand>()
          .map((command) => command.message['state']),
      contains('skipped'),
    );
  });

  test('retracting an active match reaction stops media and clears it', () {
    final controller = MatchSessionController();
    const presentation = CompanionPresentation(
      expression: 'excited',
      motion: 'cheer',
      voiceStyle: 'excited',
      voiceEnergy: 0.8,
      voiceSpeed: 1,
      hold: Duration(seconds: 2),
      returnMode: 'decay_to_focus',
    );
    expect(
      controller.receiveReply(
        text: '进球了！',
        detail: '',
        source: 'match_reaction',
        eventId: 'goal-1',
        deliveryKey: 'goal-1:1:confirmed',
        traceId: 'trace-goal',
        presentation: presentation,
      ),
      isTrue,
    );
    controller.takeCommands();
    controller.dispatch(const MatchFactSessionEvent(
      'fact-goal-1',
      2,
      retracted: true,
      relatedIds: {'goal-1'},
      continuousEnabled: true,
    ));
    expect(controller.state.activePresentation, isNull);
    expect(controller.state.phase, MatchSessionPhase.listening);
    expect(controller.takeCommands(), contains(isA<PauseAudioCommand>()));
  });

  test('owns reply, transcript and subtitle fallback view state', () {
    final controller = MatchSessionController();
    controller.setReply('进球了！', detail: '第 24 分钟');
    controller.setUserText('刚才发生了什么？');
    controller.setSubtitleFallback(true);

    expect(controller.state.replyText, '进球了！');
    expect(controller.state.replyDetail, '第 24 分钟');
    expect(controller.state.userText, '刚才发生了什么？');
    expect(controller.state.subtitleFallback, isTrue);
  });

  test('left state clears active media and keeps the last readable reply', () {
    final controller = MatchSessionController();
    controller.setReply('全场结束');
    controller.beginEntry();
    controller.entered();
    controller.beginLeaving();
    controller.left();

    expect(controller.state.presence, MatchSessionPresence.left);
    expect(controller.state.pendingAudioCount, 0);
    expect(controller.state.activeTraceId, isNull);
    expect(controller.state.replyText, '全场结束');
  });

  test('projects snapshots, events and versioned clock ticks', () {
    final controller = MatchSessionController();
    final now = DateTime.utc(2026, 9, 8, 20);
    controller.dispatch(MatchSnapshotSessionEvent({
      'homeTeam': '利物浦',
      'awayTeam': '切尔西',
      'score': {'home': 1, 'away': 0},
      'matchClock': {
        'period': 'first_half',
        'elapsedSeconds': 600,
        'running': true,
        'anchorAt': now.toIso8601String(),
        'version': 3,
      },
    }, now));
    expect(controller.state.match.homeTeam, '利物浦');
    expect(controller.state.match.clock, '10:00');

    controller.tickClock(now.add(const Duration(seconds: 5)));
    expect(controller.state.match.clock, '10:05');
    controller.dispatch(MatchClockSessionEvent({
      'period': 'first_half',
      'elapsedSeconds': 120,
      'running': true,
      'anchorAt': now.toIso8601String(),
      'version': 2,
    }, now));
    expect(controller.state.match.clock, '10:05');

    controller.dispatch(MatchEventProjectionSessionEvent(
      snapshot: {
        'score': {'home': 2, 'away': 0},
        'matchClock': {
          'period': 'first_half',
          'elapsedSeconds': 60,
          'running': true,
          'anchorAt': now.toIso8601String(),
          'version': 1,
        },
      },
      event: {
        'eventType': 'goal',
        'clock': "11'",
        'playerName': '萨拉赫',
        'score': {'home': 2, 'away': 0},
      },
      isNewEvent: true,
      now: now,
    ));
    expect(controller.state.match.homeScore, 2);
    expect(controller.state.match.clock, '10:05');
    expect(controller.state.matchClock?.version, 3);
    expect(controller.state.match.recentEventLabels.first, contains('萨拉赫'));
  });

  test('projects consecutive match events with distinct deliveries', () {
    final controller = MatchSessionController();
    final now = DateTime.utc(2026, 9, 8, 20);
    for (var index = 1; index <= 2; index++) {
      controller.dispatch(MatchEventProjectionSessionEvent(
        event: {
          'eventType': 'shot',
          'clock': '24:0$index',
          'description': '事实延迟样本 $index',
        },
        snapshot: {
          'homeTeam': '西班牙',
          'awayTeam': '德国',
          'score': {'home': 0, 'away': 0},
          'period': 'first_half',
          'clock': '12:00',
          'recentEvents': [
            {
              'eventType': 'shot',
              'clock': '24:0$index',
              'description': '事实延迟样本 $index',
            },
          ],
        },
        deliveryKey: 'evt_$index:1:confirmed',
        now: now,
      ));
    }
    expect(controller.state.match.recentEventLabels.first, '24:02 · 事实延迟样本 2');
    expect(
        controller.state.match.statusCarouselItems.first, '24:02 · 事实延迟样本 2');
  });

  test('owns presentation activation and ignores stale return timers', () {
    final controller = MatchSessionController();
    const first = CompanionPresentation(
      expression: 'excited',
      motion: 'cheer',
      voiceStyle: 'excited',
      voiceEnergy: 0.8,
      voiceSpeed: 1.05,
      hold: Duration(seconds: 2),
      returnMode: 'decay_to_focus',
    );
    const second = CompanionPresentation(
      expression: 'quiet',
      motion: 'settle',
      voiceStyle: 'quiet',
      voiceEnergy: 0.3,
      voiceSpeed: 0.95,
      hold: Duration(seconds: 2),
      returnMode: 'decay_to_listening',
    );
    controller.activatePresentation(first);
    controller.activatePresentation(second);
    controller.returnPresentation(first);
    expect(controller.state.expression, 'quiet');
    expect(controller.state.activePresentation, same(second));

    controller.returnPresentation(second);
    expect(controller.state.expression, 'listening');
    expect(controller.state.motion, 'listen');
    expect(controller.state.activePresentation, isNull);
  });

  test('projects selected and active audio input devices', () {
    final controller = MatchSessionController();
    controller.setAudioInputs(const [
      AudioInputViewData(id: '', label: '系统默认麦克风'),
      AudioInputViewData(id: 'mic-1', label: '桌面麦克风'),
      AudioInputViewData(id: 'mic-2', label: '耳机麦克风'),
    ], selectedId: 'mic-1', activeId: '');
    expect(controller.audioInputLabel, '桌面麦克风');

    controller.setAudioInputs(controller.state.audioInputDevices,
        selectedId: 'mic-1', activeId: 'mic-2');
    expect(controller.audioInputLabel, '耳机麦克风');
    controller.selectAudioInput('mic-2');
    expect(controller.state.selectedAudioInputId, 'mic-2');
  });
}
