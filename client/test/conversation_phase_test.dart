import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/screens/match_screen.dart';
import 'package:qiuqiu/services/streaming_transcription.dart';

void main() {
  test('capture readiness does not hide pending transcription work', () {
    expect(
      phaseWhenCaptureReady(
        ConversationPhase.understanding,
        hasPendingTranscript: true,
      ),
      ConversationPhase.understanding,
    );
    expect(
      phaseWhenCaptureReady(
        ConversationPhase.idle,
        hasPendingTranscript: true,
      ),
      ConversationPhase.idle,
    );
    expect(
      phaseWhenCaptureReady(
        ConversationPhase.idle,
        hasPendingTranscript: false,
      ),
      ConversationPhase.listening,
    );
  });

  test('pending final state follows the transcription lifecycle', () {
    final transcription = StreamingTranscription();
    bool send(Map<String, dynamic> _) => true;

    expect(transcription.hasPendingFinal, isFalse);
    transcription.startCapture(
      utteranceId: 'utterance-state',
      signalId: 'signal-state',
      userId: 'user-state',
      send: send,
    );
    expect(transcription.hasPendingFinal, isFalse);

    transcription.finish(Uint8List.fromList([1, 2]), send);
    expect(transcription.hasPendingFinal, isTrue);

    transcription.accept({
      'type': 'transcript_final',
      'utteranceId': 'utterance-state',
      'revision': 1,
      'text': '完整的一句话',
    });
    expect(transcription.hasPendingFinal, isFalse);
  });
}
