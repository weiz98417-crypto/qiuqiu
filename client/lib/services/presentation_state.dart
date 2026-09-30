import 'dart:convert';

import 'package:flutter/services.dart' show rootBundle;

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

  /// Expression name -> expression file index in the model3.json — the
  /// synchronous fallback mirror of presentation-map.json's `expressions`
  /// table (ADR-0007 single source; the live copy is served by
  /// [loadPresentationMap], and presentation_whitelist_contract_test.dart
  /// locks this const to the JSON three-way with the model asset). The model
  /// ships expressions/expression1-7.exp3.json; index 0 is the EMPTY
  /// expression file, so only neutral faces may bind to it.
  static const expressionIndices = {
    'focus': 0,
    'idle': 0,
    'listening': 0,
    'excited': 1,
    'thinking': 3,
    'chat': 3,
    'tease': 3,
    'happy': 3,
    'nervous': 4,
    'sad': 4,
    'confused': 5,
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
    // presentation-map.json names two listen/idle-group motions outside its
    // motions section (delivery.interrupted "confused/listening" and
    // events.var_overturn "surprised/confused"); mirror of the Go
    // clientMotionAliases entries that absorb both names.
    'listening': 'listen',
    'confused': 'idle',
  };

  /// Legacy synthetic motion names pre-dating the full motion pack -> the
  /// canonical presentation-map.json motions key they render as. Mirrored by
  /// the JS surfaces' alias shims (assets/live2d/live2d.html and the embedded
  /// page in lib/widgets/live2d_view.dart); the contract test locks every
  /// target to the JSON's motions table.
  static const legacyMotionNames = {
    'celebrate_01': 'celebrate',
    'cheer': 'celebrate',
    'focus': 'listen_02',
  };

  /// The presentation-map.json acts rows carrying a `holdLastFrame` slot
  /// (live2d-engine-swap 6.3): act name -> the quadrant keys whose body
  /// plays the motion through and holds its last frame instead of dissolving
  /// back into the resting pose; the ReturnMode decay or the next
  /// presentation apply clears the hold. The acts rows are backend-routed
  /// (the Go plan carries the flag on the wire), so this mirror exists for
  /// the ADR-0007 three-way lock —
  /// presentation_whitelist_contract_test.dart and
  /// backend/internal/relationship/presentation_table_test.go fail loudly
  /// when any of Dart == JSON == Go drifts. Opened on the celebration
  /// (positive) and chagrin (negative) ActReact rows first; extend
  /// deliberately, act by act.
  static const actsHoldLastFrame = <String, List<String>>{
    'ActReact': ['positive', 'negative'],
  };

  /// Motion name -> (motion group, variant index) inside the model3.json —
  /// the synchronous fallback mirror of presentation-map.json's `motions`
  /// table (ADR-0007; locked to it by the contract test). Group names
  /// (`idle`, `listen`, `speak`, `celebrate`) are not rows here: the
  /// rendering surfaces resolve them by picking among the group's variants.
  static const motionVariants = {
    'hello': ('hello', 0),
    'idle_01': ('idle', 0),
    'idle_02': ('idle', 1),
    'idle_03': ('idle', 2),
    'listen_01': ('listen', 0),
    'listen_02': ('listen', 1),
    'speak_01': ('speak', 0),
    'speak_02': ('speak', 1),
    'think': ('think', 0),
    // live2d-motion-revert：语义名保留，落点收敛到原装 5 组。
    'celebrate': ('speak', 0),
    'celebrate_02': ('speak', 1),
    'miss': ('idle', 2),
    'complain': ('speak', 0),
    'analysis': ('think', 0),
    'tense': ('think', 0),
    'agree': ('listen', 0),
    'wave': ('hello', 0),
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

  /// True when the backend routed this turn through an acts row carrying
  /// `holdLastFrame: true` (live2d-engine-swap 6.3): the rendering surfaces
  /// hold the motion's last frame after it finishes. Orthogonal to
  /// [returnMode] — the hold ends when the ReturnMode decay or the next
  /// presentation apply preempts it.
  final bool holdLastFrame;

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
    this.holdLastFrame = false,
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
      holdLastFrame: holdLastFrame,
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
    final motion = motionAliases[rawMotion] ?? rawMotion;
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
      holdLastFrame: presentation['holdLastFrame'] == true,
      valence:
          affect is Map ? (affect['valence'] as num?)?.toDouble() : null,
      arousal:
          affect is Map ? (affect['arousal'] as num?)?.toDouble() : null,
    );
  }
}

/// Resting body (expression, motion) a presentation decays into once its
/// HoldMS hold elapses — one real target per ReturnMode (ADR-0007 ownership
/// rules): `watching`/`decay_to_focus` keep the terminal watching focus,
/// `decay_to_listening` is the voice session waiting for the user's next
/// utterance (pairs with the phases.user_speaking listen pose), and
/// `decay_to_idle` hands the body to the C4 idle tier picker.
(String, String) presentationReturnState(
  CompanionPresentation presentation,
) {
  switch (presentation.returnMode) {
    case 'watching':
    case 'decay_to_focus':
      return ('focus', 'focus');
    case 'decay_to_listening':
      return ('listening', 'listen_01');
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

/// The client-owned slice of presentation-map.json (ADR-0007 single source):
/// expression name -> file index, motion name -> (group, variant), and the
/// turn-phase -> performance rows the client phase state machine renders.
/// (The acts/events rows are backend-routed; `delivery` reactions arrive via
/// the delivery observer.)
class PresentationMap {
  /// Asset path of the single-source mapping file (pubspec ships
  /// assets/live2d/models/qiuqiu/ as an asset directory).
  static const assetPath = 'assets/live2d/models/qiuqiu/presentation-map.json';

  /// The client-origin events row (wake-word-kws 10.3): fired by
  /// WakeService hits, rendered by the match screen via [eventFor].
  static const wakeEventClass = 'wake';

  /// Synchronous fallback mirroring the asset: hand copies are unavoidable
  /// for const contexts and synchronous APIs, so
  /// presentation_whitelist_contract_test.dart locks this instance to the
  /// JSON (Dart == JSON == model3.json, three-way). Every consumer starts on
  /// it and swaps to the parsed asset once [loadPresentationMap] resolves —
  /// that is the documented pattern for sync APIs needing map data.
  static const PresentationMap fallback = PresentationMap(
    expressions: {
      'focus': 0,
      'idle': 0,
      'listening': 0,
      'excited': 1,
      'thinking': 3,
      'chat': 3,
      'tease': 3,
      'happy': 3,
      'nervous': 4,
      'sad': 4,
      'confused': 5,
      'surprised': 5,
      'angry': 6,
    },
    motions: {
      'hello': ('hello', 0),
      'idle_01': ('idle', 0),
      'idle_02': ('idle', 1),
      'idle_03': ('idle', 2),
      'listen_01': ('listen', 0),
      'listen_02': ('listen', 1),
      'speak_01': ('speak', 0),
      'speak_02': ('speak', 1),
      'think': ('think', 0),
      'celebrate': ('speak', 0),
      'celebrate_02': ('speak', 1),
      'miss': ('idle', 2),
      'complain': ('speak', 0),
      'analysis': ('think', 0),
      'tense': ('think', 0),
      'agree': ('listen', 0),
      'wave': ('hello', 0),
    },
    phases: {
      'user_speaking': 'listening/listen_01',
      'understanding': 'thinking/think',
      'qiuqiu_speaking': 'chat/speak_01',
      'session_open': 'happy/hello',
      'match_end': 'happy/wave',
      'idle': 'affect-idle-tier',
    },
    events: {
      'goal': 'excited/celebrate',
      'big_chance': 'excited/celebrate_02',
      'save': 'surprised/tense',
      'miss': 'sad/miss',
      'shot_missed': 'sad/miss',
      'goal_cancelled': 'surprised/complain',
      'var_overturn': 'surprised/confused',
      'var_check': 'tense/tense',
      // 唤醒词（wake-word-kws 10.3）：client 发起的 events 行——空闲态喊
      // 「你好球球」叫醒球球，抬头看你接住视线；后端不发射 wake，Go 侧行
      // 仅为 ADR-0007 三方锁存活（presentation_table.go 同一提交改齐）。
      'wake': 'happy/listen_01',
    },
    actsHoldLastFrame: CompanionPresentation.actsHoldLastFrame,
  );

  final Map<String, int> expressions;
  final Map<String, (String, int)> motions;

  /// Phase key -> raw performance string ("expression/motion"), with the
  /// idle marker ("affect-idle-tier") kept as-is.
  final Map<String, String> phases;

  /// Backend-routed slice of the acts table (live2d-engine-swap 6.3): act
  /// name -> the quadrant keys whose row holds the motion's last frame.
  /// Locked to the JSON and CompanionPresentation.actsHoldLastFrame by the
  /// contract test.
  final Map<String, List<String>> actsHoldLastFrame;

  /// Match/client event class -> raw performance string ("expression/motion")
  /// (JSON "events"). Mostly backend-routed; `wake` is the one client-origin
  /// row (wake-word-kws 10.3) resolved by [eventFor]. Locked to the JSON by
  /// the contract test (and to the Go mirror by
  /// backend/internal/relationship/presentation_table_test.go).
  final Map<String, String> events;

  const PresentationMap({
    required this.expressions,
    required this.motions,
    required this.phases,
    this.events = const {},
    this.actsHoldLastFrame = const {},
  });

  factory PresentationMap.fromJson(Map<String, dynamic> json) {
    return PresentationMap(
      expressions: _parseExpressions(json['expressions']),
      motions: _parseMotions(json['motions']),
      phases: _parsePhases(json['phases']),
      events: _parsePhases(json['events']),
      actsHoldLastFrame: _parseActsHoldLastFrame(json['acts']),
    );
  }

  /// Resolves an events row "expression/motion"; null for an absent or
  /// malformed row (callers keep the current body).
  (String, String)? eventFor(String eventClass) {
    final raw = events[eventClass];
    if (raw == null) return null;
    final parts = raw.split('/');
    if (parts.length != 2) return null;
    return (parts[0], parts[1]);
  }

  /// Resolves a phases row "expression/motion"; null for the idle marker
  /// ("affect-idle-tier": the C4 idle tier picker owns the idle body) or an
  /// absent/malformed row.
  (String, String)? performanceFor(String phase) {
    final raw = phases[phase];
    if (raw == null) return null;
    final parts = raw.split('/');
    if (parts.length != 2) return null;
    return (parts[0], parts[1]);
  }

  static Map<String, int> _parseExpressions(Object? raw) {
    if (raw is! Map) return const {};
    return Map.unmodifiable({
      for (final entry in raw.entries)
        if (entry.value is int) entry.key.toString(): entry.value as int,
    });
  }

  static Map<String, (String, int)> _parseMotions(Object? raw) {
    if (raw is! Map) return const {};
    return Map.unmodifiable({
      for (final entry in raw.entries)
        if (_motionEntry(entry.value) case (final group, final variant))
          entry.key.toString(): (group, variant),
    });
  }

  static (String, int)? _motionEntry(Object? raw) {
    if (raw is! Map) return null;
    final group = raw['group'];
    final variant = raw['variant'];
    if (group is! String || variant is! int) return null;
    return (group, variant);
  }

  static Map<String, String> _parsePhases(Object? raw) {
    if (raw is! Map) return const {};
    return Map.unmodifiable({
      for (final entry in raw.entries)
        if (entry.value != null) entry.key.toString(): entry.value.toString(),
    });
  }

  static Map<String, List<String>> _parseActsHoldLastFrame(Object? raw) {
    if (raw is! Map) return const {};
    return Map.unmodifiable({
      for (final entry in raw.entries)
        if (entry.value is Map &&
            ((entry.value as Map)['holdLastFrame'] is List) &&
            ((entry.value as Map)['holdLastFrame'] as List).isNotEmpty)
          entry.key.toString(): [
            for (final quadrant in (entry.value as Map)['holdLastFrame'] as List)
              if (quadrant is String) quadrant,
          ],
    });
  }
}

PresentationMap? _loadedPresentationMap;

/// Loads presentation-map.json once via rootBundle (ADR-0007: the JSON is
/// the one mapping file the web page, the embedded page and the Dart layer
/// all derive from). Until the load completes — or when the asset is missing
/// or malformed — callers get [PresentationMap.fallback], the synchronous
/// mirror locked to the JSON by the contract test.
Future<PresentationMap> loadPresentationMap() async {
  final cached = _loadedPresentationMap;
  if (cached != null) return cached;
  PresentationMap parsed;
  try {
    final raw = await rootBundle.loadString(PresentationMap.assetPath);
    parsed =
        PresentationMap.fromJson(jsonDecode(raw) as Map<String, dynamic>);
  } catch (_) {
    parsed = PresentationMap.fallback;
  }
  return _loadedPresentationMap = parsed;
}
