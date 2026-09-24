import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_screen.dart';
import 'package:qiuqiu/services/match_session_controller.dart';

void main() {
  test('连续音频元数据按到达顺序与二进制帧配对', () {
    final queue = PendingAudioQueue();
    queue.add(const PendingAudio(mime: 'audio/wav', traceId: 'trace-1'));
    queue.add(const PendingAudio(mime: 'audio/mpeg', traceId: 'trace-2'));

    expect(queue.take()?.traceId, 'trace-1');
    expect(queue.take()?.traceId, 'trace-2');
    expect(queue.take(), isNull);
  });

  test('delivery dedupe preserves fact revisions and skips exact repeats', () {
    final deduplicator = DeliveryDeduplicator(capacity: 2);
    expect(deduplicator.remember('event-1:1:confirmed'), isTrue);
    expect(deduplicator.remember('event-1:1:confirmed'), isFalse);
    expect(deduplicator.remember('event-1:2:reconciled'), isTrue);
    expect(
      matchEventDeliveryKey({
        'id': 'event-1',
        'factRevision': 2,
        'factStatus': 'reconciled',
      }),
      'event-1:2:reconciled',
    );
  });

  test('duplicate audio metadata consumes its binary frame without playback',
      () {
    final queue = PendingAudioQueue();
    const duplicate = PendingAudio(
      mime: 'audio/mpeg',
      traceId: 'trace-duplicate',
      skip: true,
    );
    queue.add(duplicate);
    expect(queue.take()?.skip, isTrue);
    expect(mutedPlaybackReceipt(duplicate), {
      'type': 'voice_playback',
      'traceId': 'trace-duplicate',
      'state': 'skipped',
    });
  });

  test('静音跳过音频时仍生成终态回执', () {
    final receipt = mutedPlaybackReceipt(
      const PendingAudio(mime: 'audio/mpeg', traceId: 'trace-muted'),
    );

    expect(receipt, {
      'type': 'voice_playback',
      'traceId': 'trace-muted',
      'state': 'skipped',
    });
    expect(
      mutedPlaybackReceipt(const PendingAudio(mime: 'audio/mpeg')),
      isNull,
    );
  });

  test('打断播放会保留用户当前交流阶段并生成回执', () {
    final controller = MatchSessionController();
    controller.transcriptPartial('我还在说');
    controller.handlePlayback(
      'interrupted',
      traceId: 'trace-interrupted',
      continuousEnabled: true,
    );
    expect(controller.state.phase, MatchSessionPhase.userSpeaking);
    final receipt =
        controller.takeCommands().whereType<SendSocketCommand>().last.message;
    expect(receipt, {
      'type': 'voice_playback',
      'traceId': 'trace-interrupted',
      'state': 'interrupted',
    });
  });

  test('带投递键的播放终态实报 playback_result，旧路径不发', () {
    final controller = MatchSessionController();
    controller.handlePlayback(
      'ended',
      traceId: 'trace-1',
      deliveryKey: 'goal-1:1:confirmed',
      continuousEnabled: true,
    );
    final messages = controller
        .takeCommands()
        .whereType<SendSocketCommand>()
        .map((command) => command.message)
        .toList();
    expect(messages, containsAll(<Matcher>[
      equals({
        'type': 'voice_playback',
        'traceId': 'trace-1',
        'state': 'ended',
      }),
      equals({
        'type': 'playback_result',
        'deliveryKey': 'goal-1:1:confirmed',
        'state': 'completed',
      }),
    ]));

    controller.handlePlayback(
      'interrupted',
      deliveryKey: 'goal-1:1:confirmed',
      continuousEnabled: true,
      error: 'user_talk',
    );
    final interrupted = controller
        .takeCommands()
        .whereType<SendSocketCommand>()
        .map((command) => command.message)
        .where((message) => message['type'] == 'playback_result')
        .single;
    expect(interrupted, {
      'type': 'playback_result',
      'deliveryKey': 'goal-1:1:confirmed',
      'state': 'interrupted',
      'reason': 'user_talk',
    });

    // 缺 deliveryKey 的旧下发路径：不实报（向后兼容）。
    controller.handlePlayback(
      'ended',
      traceId: 'trace-2',
      continuousEnabled: true,
    );
    expect(
      controller
          .takeCommands()
          .whereType<SendSocketCommand>()
          .where((command) => command.message['type'] == 'playback_result'),
      isEmpty,
    );

    // started 非终态不报。
    controller.handlePlayback(
      'started',
      deliveryKey: 'goal-1:1:confirmed',
      continuousEnabled: true,
    );
    expect(
      controller
          .takeCommands()
          .whereType<SendSocketCommand>()
          .where((command) => command.message['type'] == 'playback_result'),
      isEmpty,
    );
  });

  test('静音与被顶替的跳过也走 playback_result 实报', () {
    final controller = MatchSessionController();
    controller.queueAudioMetadata(const PendingAudio(
      mime: 'audio/mpeg',
      traceId: 'trace-muted',
      deliveryKey: 'goal-3:1:confirmed',
    ));
    controller.consumeAudio(
      Uint8List.fromList([1]),
      soundEnabled: false,
      continuousEnabled: true,
    );
    final messages = controller
        .takeCommands()
        .whereType<SendSocketCommand>()
        .map((command) => command.message)
        .where((message) => message['type'] == 'playback_result')
        .toList();
    expect(messages, [
      {
        'type': 'playback_result',
        'deliveryKey': 'goal-3:1:confirmed',
        'state': 'skipped',
        'reason': 'muted',
      }
    ]);

    // 旧下发路径（无 deliveryKey）：跳过只走原 voice_playback 回执。
    controller.queueAudioMetadata(
        const PendingAudio(mime: 'audio/mpeg', traceId: 'trace-legacy'));
    controller.consumeAudio(
      Uint8List.fromList([2]),
      soundEnabled: false,
      continuousEnabled: true,
    );
    final legacy = controller
        .takeCommands()
        .whereType<SendSocketCommand>()
        .map((command) => command.message)
        .toList();
    expect(legacy, [
      {'type': 'voice_playback', 'traceId': 'trace-legacy', 'state': 'skipped'}
    ]);

    expect(
      playbackResultReceipt(const PendingAudio(
        mime: 'audio/mpeg',
        deliveryKey: 'goal-4:1:confirmed',
      )),
      {
        'type': 'playback_result',
        'deliveryKey': 'goal-4:1:confirmed',
        'state': 'skipped',
      },
    );
    expect(
      playbackResultReceipt(const PendingAudio(mime: 'audio/mpeg')),
      isNull,
    );
  });

  test('播放终态映射覆盖实报三态与 failed', () {
    expect(playbackResultState('ended'), 'completed');
    expect(playbackResultState('completed'), 'completed');
    expect(playbackResultState('interrupted'), 'interrupted');
    expect(playbackResultState('blocked'), 'skipped');
    expect(playbackResultState('failed'), 'failed');
    expect(playbackResultState('started'), isNull);
  });

  test('播放失败时文字兜底仍然可见', () {
    expect(
      shouldShowReplyText(
        subtitlesEnabled: false,
        playbackFallback: true,
      ),
      isTrue,
    );
    expect(shouldClearTextInput(false), isFalse);
  });
}
