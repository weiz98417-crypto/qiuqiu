import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/match_session_controller.dart';
import 'package:qiuqiu/services/streaming_transcription.dart';

void main() {
  test('capture readiness does not hide pending transcription work', () {
    final controller = MatchSessionController();
    controller.transcriptFinal('处理中');
    controller.vadListening(hasPendingTranscript: true);
    expect(controller.state.phase, MatchSessionPhase.understanding);

    final idleController = MatchSessionController();
    idleController.vadListening(hasPendingTranscript: true);
    expect(idleController.state.phase, MatchSessionPhase.idle);
    idleController.vadListening(hasPendingTranscript: false);
    expect(idleController.state.phase, MatchSessionPhase.listening);
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
