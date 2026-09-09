export '../services/presentation_state.dart';

(String, String) splitReplyForDisplay(String reply) {
  final punctuation = reply.indexOf(RegExp(r'[。！？]'));
  if (punctuation > 0 && punctuation < reply.length - 1) {
    return (
      reply.substring(0, punctuation + 1),
      reply.substring(punctuation + 1).trim(),
    );
  }
  return (reply, '');
}
