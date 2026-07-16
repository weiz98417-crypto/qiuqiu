import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_screen.dart';

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
    expect(
      phaseAfterInterrupt(ConversationPhase.userSpeaking),
      ConversationPhase.userSpeaking,
    );
    expect(
      phaseAfterInterrupt(ConversationPhase.understanding),
      ConversationPhase.understanding,
    );
    expect(
      phaseAfterInterrupt(ConversationPhase.speaking),
      ConversationPhase.listening,
    );
    expect(interruptedPlaybackReceipt('trace-interrupted'), {
      'type': 'voice_playback',
      'traceId': 'trace-interrupted',
      'state': 'interrupted',
    });
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
