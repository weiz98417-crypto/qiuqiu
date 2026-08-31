import 'dart:convert';
import 'dart:typed_data';

typedef TranscriptionSender = bool Function(Map<String, dynamic> message);

enum TranscriptUpdateKind {
  partialTranscript,
  finalTranscript,
  recoverableError,
  fallback,
}

class TranscriptUpdate {
  final TranscriptUpdateKind kind;
  final String utteranceId;
  final String text;
  final String? reason;
  final Uint8List? fallbackAudio;
  final String? fallbackSignalId;

  const TranscriptUpdate({
    required this.kind,
    required this.utteranceId,
    this.text = '',
    this.reason,
    this.fallbackAudio,
    this.fallbackSignalId,
  });
}

class TranscriptionFallback {
  final Uint8List audio;
  final String? signalId;

  const TranscriptionFallback({required this.audio, this.signalId});
}

class TranscriptionFinish {
  final bool streamed;
  final Uint8List? fallbackAudio;
  final String? fallbackSignalId;

  const TranscriptionFinish({
    required this.streamed,
    this.fallbackAudio,
    this.fallbackSignalId,
  });
}

class StreamingTranscription {
  final Map<String, TranscriptionFallback> _pendingAudio = {};
  final Map<String, int> _revisions = {};
  final Map<String, int> _utteranceOrders = {};
  String? _activeUtteranceId;
  String? _activeSignalId;
  String? _abandonedSignalId;
  int _sequence = 0;
  int _nextUtteranceOrder = 0;
  int _latestDisplayedOrder = -1;
  int _latestFinalOrder = -1;

  String? get activeUtteranceId => _activeUtteranceId;
  bool get hasPendingFinal => _pendingAudio.isNotEmpty;

  bool startCapture({
    required String utteranceId,
    required String signalId,
    required String userId,
    required TranscriptionSender send,
  }) {
    if (_activeUtteranceId != null || utteranceId.isEmpty || userId.isEmpty) {
      return false;
    }
    final sent = send({
      'type': 'asr_start',
      'utteranceId': utteranceId,
      'signalId': signalId,
      'userId': userId,
      'encoding': 'pcm_s16le',
      'sampleRate': 16000,
      'channels': 1,
      'language': 'zh',
    });
    if (!sent) return false;
    _activeUtteranceId = utteranceId;
    _activeSignalId = signalId;
    _abandonedSignalId = null;
    _sequence = 0;
    _revisions[utteranceId] = -1;
    _utteranceOrders[utteranceId] = _nextUtteranceOrder++;
    return true;
  }

  bool append(Uint8List audio, TranscriptionSender send) {
    final utteranceId = _activeUtteranceId;
    if (utteranceId == null || audio.isEmpty) return false;
    final sent = send({
      'type': 'asr_chunk',
      'utteranceId': utteranceId,
      'sequence': _sequence,
      'audio': base64Encode(audio),
    });
    if (!sent) {
      cancelActive(send, preserveSignalForFallback: true);
      return false;
    }
    _sequence += 1;
    return sent;
  }

  TranscriptionFinish finish(
    Uint8List? fullAudio,
    TranscriptionSender send,
  ) {
    final utteranceId = _activeUtteranceId;
    if (utteranceId == null) {
      return TranscriptionFinish(
        streamed: false,
        fallbackAudio: fullAudio,
        fallbackSignalId: _takeAbandonedSignalId(),
      );
    }
    if (fullAudio == null || fullAudio.isEmpty) {
      cancelActive(send);
      return TranscriptionFinish(
        streamed: false,
        fallbackAudio: fullAudio,
        fallbackSignalId: _takeAbandonedSignalId(),
      );
    }
    final signalId = _activeSignalId;
    _activeUtteranceId = null;
    _activeSignalId = null;
    _pendingAudio[utteranceId] = TranscriptionFallback(
      audio: fullAudio,
      signalId: signalId,
    );
    final sent = send({
      'type': 'asr_finish',
      'utteranceId': utteranceId,
    });
    if (sent) return const TranscriptionFinish(streamed: true);
    _pendingAudio.remove(utteranceId);
    _revisions.remove(utteranceId);
    _utteranceOrders.remove(utteranceId);
    return TranscriptionFinish(
      streamed: false,
      fallbackAudio: fullAudio,
      fallbackSignalId: signalId,
    );
  }

  TranscriptUpdate? accept(Map<String, dynamic> message) {
    final type = message['type']?.toString() ?? '';
    final utteranceId = message['utteranceId']?.toString() ?? '';
    if (!_revisions.containsKey(utteranceId)) {
      return null;
    }
    final utteranceOrder = _utteranceOrders[utteranceId]!;
    if (type == 'transcript_partial') {
      final revision = message['revision'];
      if (revision is! num || revision.toInt() <= _revisions[utteranceId]!) {
        return null;
      }
      if (utteranceOrder < _latestDisplayedOrder) return null;
      _latestDisplayedOrder = utteranceOrder;
      _revisions[utteranceId] = revision.toInt();
      return TranscriptUpdate(
        kind: TranscriptUpdateKind.partialTranscript,
        utteranceId: utteranceId,
        text: message['text']?.toString() ?? '',
      );
    }
    if (type == 'transcript_error') {
      final recoverable = message['recoverable'] == true;
      if (recoverable) {
        if (utteranceOrder < _latestDisplayedOrder) return null;
        return TranscriptUpdate(
          kind: TranscriptUpdateKind.recoverableError,
          utteranceId: utteranceId,
          reason: message['reason']?.toString(),
        );
      }
      if (_activeUtteranceId == utteranceId) {
        _abandonedSignalId = _activeSignalId;
        _activeUtteranceId = null;
        _activeSignalId = null;
      }
      final fallbackAudio = _pendingAudio.remove(utteranceId);
      _revisions.remove(utteranceId);
      final fallback = fallbackAudio;
      _utteranceOrders.remove(utteranceId);
      if (utteranceOrder < _latestFinalOrder) {
        _takeAbandonedSignalId();
        return null;
      }
      return TranscriptUpdate(
        kind: TranscriptUpdateKind.fallback,
        utteranceId: utteranceId,
        reason: message['reason']?.toString(),
        fallbackAudio: fallback?.audio,
        fallbackSignalId: fallback?.signalId ?? _takeAbandonedSignalId(),
      );
    }
    if (type != 'transcript_final') return null;
    _pendingAudio.remove(utteranceId);
    _revisions.remove(utteranceId);
    _utteranceOrders.remove(utteranceId);
    if (utteranceOrder < _latestDisplayedOrder) return null;
    _latestDisplayedOrder = utteranceOrder;
    _latestFinalOrder = utteranceOrder;
    return TranscriptUpdate(
      kind: TranscriptUpdateKind.finalTranscript,
      utteranceId: utteranceId,
      text: message['text']?.toString() ?? '',
    );
  }

  List<TranscriptionFallback> cancelAll(TranscriptionSender send) {
    final fallbackAudio = _pendingAudio.values.toList(growable: false);
    final utteranceIds = <String>{
      ..._pendingAudio.keys,
      if (_activeUtteranceId != null) _activeUtteranceId!,
    };
    for (final utteranceId in utteranceIds) {
      send({'type': 'asr_cancel', 'utteranceId': utteranceId});
    }
    if (_activeSignalId != null) {
      _abandonedSignalId = _activeSignalId;
    }
    _activeUtteranceId = null;
    _activeSignalId = null;
    _sequence = 0;
    _pendingAudio.clear();
    _revisions.clear();
    _utteranceOrders.clear();
    _latestDisplayedOrder = -1;
    _latestFinalOrder = -1;
    return fallbackAudio;
  }

  void cancelActive(
    TranscriptionSender send, {
    bool preserveSignalForFallback = false,
  }) {
    final utteranceId = _activeUtteranceId;
    if (utteranceId == null) return;
    send({'type': 'asr_cancel', 'utteranceId': utteranceId});
    _abandonedSignalId = preserveSignalForFallback ? _activeSignalId : null;
    _activeUtteranceId = null;
    _activeSignalId = null;
    _sequence = 0;
    _revisions.remove(utteranceId);
    _utteranceOrders.remove(utteranceId);
  }

  String? _takeAbandonedSignalId() {
    final signalId = _abandonedSignalId;
    _abandonedSignalId = null;
    return signalId;
  }
}
