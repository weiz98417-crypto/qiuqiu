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

class CompanionPresentation {
  final String expression;
  final String motion;
  final String voiceStyle;
  final double voiceEnergy;
  final double voiceSpeed;
  final Duration hold;
  final String returnMode;

  const CompanionPresentation({
    required this.expression,
    required this.motion,
    required this.voiceStyle,
    required this.voiceEnergy,
    required this.voiceSpeed,
    required this.hold,
    required this.returnMode,
  });

  CompanionPresentation copyWith({String? returnMode, double? voiceSpeed}) {
    return CompanionPresentation(
      expression: expression,
      motion: motion,
      voiceStyle: voiceStyle,
      voiceEnergy: voiceEnergy,
      voiceSpeed: voiceSpeed ?? this.voiceSpeed,
      hold: hold,
      returnMode: returnMode ?? this.returnMode,
    );
  }

  static CompanionPresentation? fromReplyData(Map<String, dynamic>? data) {
    final raw = data?['presentation'];
    if (raw is! Map) return null;
    final presentation = Map<String, dynamic>.from(raw);
    final expression = presentation['expression'] as String? ?? '';
    final motion = presentation['motion'] as String? ?? '';
    if (expression.isEmpty || motion.isEmpty) return null;
    final holdMs = (presentation['holdMs'] as num?)?.toInt() ?? 1800;
    return CompanionPresentation(
      expression: expression,
      motion: motion,
      voiceStyle: presentation['voiceStyle'] as String? ?? 'natural',
      voiceEnergy: (presentation['voiceEnergy'] as num?)?.toDouble() ?? 0.5,
      voiceSpeed: (presentation['voiceSpeed'] as num?)?.toDouble() ?? 1,
      hold: Duration(milliseconds: holdMs.clamp(0, 10000)),
      returnMode: presentation['returnMode'] as String? ?? 'decay_to_focus',
    );
  }
}

(String, String) presentationReturnState(
  CompanionPresentation presentation,
) {
  switch (presentation.returnMode) {
    case 'decay_to_listening':
      return ('listening', 'listen');
    case 'decay_to_idle':
      return ('idle', 'idle');
    default:
      return ('focus', 'focus');
  }
}
