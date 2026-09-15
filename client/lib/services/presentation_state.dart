import 'idle_tier_picker.dart';

/// Presentation contract between the backend PresentationPlan and the Live2D
/// body (ADR-0005: PresentationPlan stays the single contract; C4 widened the
/// whitelist from 7 to all 12 motion groups / 17 motions of
/// assets/live2d/models/qiuqiu/female_01Arkit_6.model3.json).
///
/// The same whitelist is mirrored Go-side in
/// backend/internal/relationship/presentation_vocabulary.go; the contract is
/// locked from both directions by client/test/presentation_whitelist_test.dart
/// and backend/internal/relationship/presentation_vocabulary_test.go.
class CompanionPresentation {
  /// Expression names the client accepts; anything else (after aliasing) is
  /// strictly rejected.
  static const allowedExpressions = {
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

  /// Legacy backend expression vocabulary absorbed into canonical names.
  static const expressionAliases = {
    'low': 'sad',
    'tense': 'nervous',
    'deflated': 'sad',
  };

  /// Expression name -> expression file index in the model3.json, mirroring
  /// the exprMap in lib/widgets/live2d_view.dart and assets/live2d/live2d.html
  /// (the model ships expressions/expression1-7.exp3.json).
  static const expressionIndices = {
    'idle': 0,
    'listening': 0,
    'confused': 0,
    'thinking': 2,
    'focus': 2,
    'excited': 1,
    'chat': 3,
    'tease': 3,
    'happy': 3,
    'nervous': 4,
    'sad': 4,
    'complain': 4,
    'surprised': 5,
    'angry': 6,
  };

  /// Motion names the client accepts: all 12 motion groups, every individual
  /// motion inside multi-motion groups (17 motions total), plus the legacy
  /// synthetic names kept for backends predating the full motion pack.
  /// Unknown names are strictly rejected.
  static const allowedMotions = {
    // The 12 motion groups of female_01Arkit_6.model3.json.
    'hello',
    'idle',
    'listen',
    'speak',
    'think',
    'celebrate',
    'miss',
    'complain',
    'analysis',
    'tense',
    'agree',
    'wave',
    // Individual variants inside multi-motion groups.
    'idle_01',
    'idle_02',
    'idle_03',
    'listen_01',
    'listen_02',
    'speak_01',
    'speak_02',
    'celebrate_01',
    'celebrate_02',
    // Legacy synthetic names pre-dating the full motion pack.
    'cheer',
    'focus',
  };

  /// Legacy backend motion vocabulary absorbed into canonical names.
  static const motionAliases = {
    'hold': 'focus',
    'settle': 'idle',
    'slump': 'idle',
    'nod': 'agree',
  };

  /// Motion name -> (motion group, variant index) inside the model3.json,
  /// mirroring the playMotion map in lib/widgets/live2d_view.dart and the
  /// motionGroups map in assets/live2d/live2d.html.
  static const motionVariants = {
    'hello': ('hello', 0),
    'idle': ('idle', 0),
    'idle_01': ('idle', 0),
    'idle_02': ('idle', 1),
    'idle_03': ('idle', 2),
    'listen': ('listen', 0),
    'listen_01': ('listen', 0),
    'listen_02': ('listen', 1),
    'speak': ('speak', 0),
    'speak_01': ('speak', 0),
    'speak_02': ('speak', 1),
    'think': ('think', 0),
    'celebrate': ('celebrate', 0),
    'celebrate_01': ('celebrate', 0),
    'celebrate_02': ('celebrate', 1),
    'miss': ('miss', 0),
    'complain': ('complain', 0),
    'analysis': ('analysis', 0),
    'tense': ('tense', 0),
    'agree': ('agree', 0),
    'wave': ('wave', 0),
    // Legacy synthetic names.
    'cheer': ('celebrate', 0),
    'focus': ('listen', 1),
  };

  static const allowedVoiceStyles = {
    'natural',
    'quiet',
    'warm',
    'excited',
    'tense',
    'low_disappointed',
    'calm',
    'soft',
  };
  static const allowedReturnModes = {
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

  /// Affect vector from the backend PresentationPlan (nullable for legacy
  /// senders); feeds the idle tier picker (see idle_tier_picker.dart).
  final double? valence;
  final double? arousal;

  const CompanionPresentation({
    required this.expression,
    required this.motion,
    required this.voiceStyle,
    required this.voiceEnergy,
    required this.voiceSpeed,
    required this.hold,
    required this.returnMode,
    this.valence,
    this.arousal,
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
      valence: valence,
      arousal: arousal,
    );
  }

  static String? normalizeExpression(String? value) {
    final raw = value?.trim() ?? '';
    final normalized = expressionAliases[raw] ?? raw;
    return allowedExpressions.contains(normalized) ? normalized : null;
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
        !allowedMotions.contains(motion) ||
        !allowedVoiceStyles.contains(voiceStyle) ||
        !allowedReturnModes.contains(returnMode)) {
      return null;
    }
    final holdMs = (presentation['holdMs'] as num?)?.toInt() ?? 1800;
    final affect = presentation['affect'];
    return CompanionPresentation(
      expression: expression,
      motion: motion,
      voiceStyle: voiceStyle,
      voiceEnergy: (presentation['voiceEnergy'] as num?)?.toDouble() ?? 0.5,
      voiceSpeed: (presentation['voiceSpeed'] as num?)?.toDouble() ?? 1,
      hold: Duration(milliseconds: holdMs.clamp(0, 10000)),
      returnMode: returnMode,
      valence:
          affect is Map ? (affect['valence'] as num?)?.toDouble() : null,
      arousal:
          affect is Map ? (affect['arousal'] as num?)?.toDouble() : null,
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
      final tier = IdleTierPicker.tierFor(
        valence: presentation.valence ?? 0,
        arousal: presentation.arousal ?? 0.2,
      );
      return ('idle', IdleTierPicker.motionFor(tier));
    default:
      return ('focus', 'focus');
  }
}
