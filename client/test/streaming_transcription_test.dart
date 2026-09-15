import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/streaming_transcription.dart';

void main() {
  test('streams one utterance in order and keeps full audio until final', () {
    final sent = <Map<String, dynamic>>[];
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> message) {
      sent.add(message);
      return true;
    }

    expect(
      transcription.startCapture(
        utteranceId: 'utterance-1',
        signalId: 'signal-1',
        userId: 'user-1',
        send: send,
      ),
      isTrue,
    );
    expect(transcription.append(Uint8List.fromList([1, 2]), send), isTrue);
    expect(
      transcription.finish(Uint8List.fromList([1, 2, 3, 4]), send).streamed,
      isTrue,
    );

    expect(sent, [
      {
        'type': 'asr_start',
        'utteranceId': 'utterance-1',
        'signalId': 'signal-1',
        'userId': 'user-1',
        'encoding': 'pcm_s16le',
        'sampleRate': 16000,
        'channels': 1,
        'language': 'zh',
      },
      {
        'type': 'asr_chunk',
        'utteranceId': 'utterance-1',
        'sequence': 0,
        'audio': 'AQI=',
      },
      {'type': 'asr_finish', 'utteranceId': 'utterance-1'},
    ]);

    final update = transcription.accept({
      'type': 'transcript_final',
      'utteranceId': 'utterance-1',
      'revision': 2,
      'text': '利物浦这球踢得漂亮',
    });
    expect(update?.kind, TranscriptUpdateKind.finalTranscript);
    expect(update?.text, '利物浦这球踢得漂亮');
    expect(update?.fallbackAudio, isNull);
  });

  test('publishes newer partial revisions and lets final text win', () {
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> _) => true;
    transcription.startCapture(
      utteranceId: 'utterance-2',
      signalId: 'signal-2',
      userId: 'user-1',
      send: send,
    );

    final first = transcription.accept({
      'type': 'transcript_partial',
      'utteranceId': 'utterance-2',
      'revision': 1,
      'text': '利物浦这球',
    });
    final stale = transcription.accept({
      'type': 'transcript_partial',
      'utteranceId': 'utterance-2',
      'revision': 1,
      'text': '旧结果',
    });
    transcription.finish(Uint8List.fromList([1, 2]), send);
    final finalUpdate = transcription.accept({
      'type': 'transcript_final',
      'utteranceId': 'utterance-2',
      'revision': 2,
      'text': '利物浦这球踢得漂亮',
    });

    expect(first?.kind, TranscriptUpdateKind.partialTranscript);
    expect(first?.text, '利物浦这球');
    expect(stale, isNull);
    expect(finalUpdate?.kind, TranscriptUpdateKind.finalTranscript);
    expect(finalUpdate?.text, '利物浦这球踢得漂亮');
  });

  test('keeps streaming after recoverable errors and returns audio on failure',
      () {
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> _) => true;
    transcription.startCapture(
      utteranceId: 'utterance-3',
      signalId: 'signal-3',
      userId: 'user-1',
      send: send,
    );
    transcription.finish(Uint8List.fromList([7, 8, 9]), send);

    final recoverable = transcription.accept({
      'type': 'transcript_error',
      'utteranceId': 'utterance-3',
      'reason': 'partial recognition timed out',
      'recoverable': true,
    });
    final failed = transcription.accept({
      'type': 'transcript_error',
      'utteranceId': 'utterance-3',
      'reason': 'recognition unavailable',
      'recoverable': false,
    });

    expect(recoverable?.kind, TranscriptUpdateKind.recoverableError);
    expect(recoverable?.fallbackAudio, isNull);
    expect(failed?.kind, TranscriptUpdateKind.fallback);
    expect(failed?.fallbackAudio, Uint8List.fromList([7, 8, 9]));
    expect(failed?.fallbackSignalId, 'signal-3');
  });

  test('cancels active and pending utterances when capture stops', () {
    final sent = <Map<String, dynamic>>[];
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> message) {
      sent.add(message);
      return true;
    }

    transcription.startCapture(
      utteranceId: 'utterance-4',
      signalId: 'signal-4',
      userId: 'user-1',
      send: send,
    );
    transcription.finish(Uint8List.fromList([1, 2]), send);
    transcription.startCapture(
      utteranceId: 'utterance-5',
      signalId: 'signal-5',
      userId: 'user-1',
      send: send,
    );

    final fallbacks = transcription.cancelAll(send);

    expect(
      sent.where((message) => message['type'] == 'asr_cancel'),
      containsAll([
        {'type': 'asr_cancel', 'utteranceId': 'utterance-4'},
        {'type': 'asr_cancel', 'utteranceId': 'utterance-5'},
      ]),
    );
    expect(transcription.activeUtteranceId, isNull);
    expect(fallbacks.single.audio, Uint8List.fromList([1, 2]));
    expect(fallbacks.single.signalId, 'signal-4');
  });

  test('falls back to full audio after any chunk send failure', () {
    final sentTypes = <String>[];
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> message) {
      final type = message['type'] as String;
      sentTypes.add(type);
      return type != 'asr_chunk';
    }

    transcription.startCapture(
      utteranceId: 'utterance-6',
      signalId: 'signal-6',
      userId: 'user-1',
      send: send,
    );

    expect(transcription.append(Uint8List.fromList([1, 2]), send), isFalse);
    final finish = transcription.finish(Uint8List.fromList([1, 2]), send);

    expect(transcription.activeUtteranceId, isNull);
    expect(finish.streamed, isFalse);
    expect(finish.fallbackAudio, Uint8List.fromList([1, 2]));
    expect(finish.fallbackSignalId, 'signal-6');
    expect(sentTypes, ['asr_start', 'asr_chunk', 'asr_cancel']);
  });

  test('does not let a late older final replace newer displayed speech', () {
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> _) => true;
    transcription.startCapture(
      utteranceId: 'utterance-7',
      signalId: 'signal-7',
      userId: 'user-1',
      send: send,
    );
    transcription.finish(Uint8List.fromList([1, 2]), send);
    transcription.startCapture(
      utteranceId: 'utterance-8',
      signalId: 'signal-8',
      userId: 'user-1',
      send: send,
    );
    final newerPartial = transcription.accept({
      'type': 'transcript_partial',
      'utteranceId': 'utterance-8',
      'revision': 1,
      'text': '第二句',
    });
    final olderFinal = transcription.accept({
      'type': 'transcript_final',
      'utteranceId': 'utterance-7',
      'revision': 2,
      'text': '第一句',
    });

    expect(newerPartial?.text, '第二句');
    expect(olderFinal, isNull);
  });

  test('does not replay an older failed utterance after newer final speech',
      () {
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> _) => true;
    transcription.startCapture(
      utteranceId: 'utterance-9',
      signalId: 'signal-9',
      userId: 'user-1',
      send: send,
    );
    transcription.finish(Uint8List.fromList([1, 2]), send);
    transcription.startCapture(
      utteranceId: 'utterance-10',
      signalId: 'signal-10',
      userId: 'user-1',
      send: send,
    );
    transcription.accept({
      'type': 'transcript_partial',
      'utteranceId': 'utterance-10',
      'revision': 1,
      'text': '更新的一句',
    });
    transcription.accept({
      'type': 'transcript_final',
      'utteranceId': 'utterance-10',
      'revision': 2,
      'text': '更新的一句',
    });

    final olderFailure = transcription.accept({
      'type': 'transcript_error',
      'utteranceId': 'utterance-9',
      'recoverable': false,
      'reason': 'final recognition failed',
    });

    expect(olderFailure, isNull);
  });

  test('keeps older failure fallback while newer utterance is only partial',
      () {
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> _) => true;
    transcription.startCapture(
      utteranceId: 'utterance-11',
      signalId: 'signal-11',
      userId: 'user-1',
      send: send,
    );
    transcription.finish(Uint8List.fromList([3, 4]), send);
    transcription.startCapture(
      utteranceId: 'utterance-12',
      signalId: 'signal-12',
      userId: 'user-1',
      send: send,
    );
    transcription.accept({
      'type': 'transcript_partial',
      'utteranceId': 'utterance-12',
      'revision': 1,
      'text': '新句只有 partial',
    });

    final olderFailure = transcription.accept({
      'type': 'transcript_error',
      'utteranceId': 'utterance-11',
      'recoverable': false,
      'reason': 'final recognition failed',
    });

    expect(olderFailure?.kind, TranscriptUpdateKind.fallback);
    expect(olderFailure?.fallbackAudio, Uint8List.fromList([3, 4]));
    expect(olderFailure?.fallbackSignalId, 'signal-11');
  });
}
