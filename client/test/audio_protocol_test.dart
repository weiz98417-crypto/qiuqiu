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
}
