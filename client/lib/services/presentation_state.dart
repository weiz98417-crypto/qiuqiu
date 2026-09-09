class CompanionPresentation {
  static const _allowedExpressions = {
    'idle',
    'listening',
    'focus',
    'thinking',
    'confused',
    'excited',
    'chat',
    'tease',
    'happy',
    'nervous',
    'sad',
    'surprised',
    'angry',
  };
  static const _expressionAliases = {
    'low': 'sad',
    'tense': 'nervous',
    'deflated': 'sad',
  };
  static const _allowedMotions = {
    'hello',
    'cheer',
    'idle',
    'listen',
    'focus',
    'speak',
    'think',
  };
  static const _motionAliases = {
    'hold': 'focus',
    'settle': 'idle',
    'slump': 'idle',
  };
  static const _allowedVoiceStyles = {
    'natural',
    'quiet',
    'warm',
    'excited',
    'tense',
    'low_disappointed',
    'calm',
    'soft',
  };
  static const _allowedReturnModes = {
    'decay_to_focus',
    'decay_to_listening',
    'decay_to_idle',
    'watching',
  };
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

  static String? normalizeExpression(String? value) {
    final raw = value?.trim() ?? '';
    final normalized = _expressionAliases[raw] ?? raw;
    return _allowedExpressions.contains(normalized) ? normalized : null;
  }

  static CompanionPresentation? fromReplyData(Map<String, dynamic>? data) {
    final raw = data?['presentation'];
    if (raw is! Map) return null;
    final presentation = Map<String, dynamic>.from(raw);
    final rawExpression = presentation['expression'] as String? ?? '';
    final rawMotion = presentation['motion'] as String? ?? '';
    final expression = normalizeExpression(rawExpression);
    final motion = _motionAliases[rawMotion] ?? rawMotion;
    final voiceStyle = presentation['voiceStyle'] as String? ?? 'natural';
    final returnMode =
        presentation['returnMode'] as String? ?? 'decay_to_focus';
    if (expression == null ||
        !_allowedMotions.contains(motion) ||
        !_allowedVoiceStyles.contains(voiceStyle) ||
        !_allowedReturnModes.contains(returnMode)) {
      return null;
    }
    final holdMs = (presentation['holdMs'] as num?)?.toInt() ?? 1800;
    return CompanionPresentation(
      expression: expression,
      motion: motion,
      voiceStyle: voiceStyle,
      voiceEnergy: (presentation['voiceEnergy'] as num?)?.toDouble() ?? 0.5,
      voiceSpeed: (presentation['voiceSpeed'] as num?)?.toDouble() ?? 1,
      hold: Duration(milliseconds: holdMs.clamp(0, 10000)),
      returnMode: returnMode,
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
