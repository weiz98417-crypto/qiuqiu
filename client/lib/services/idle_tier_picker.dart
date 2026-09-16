/// Idle body states for quiet stretches (design.md C4): the affect vector of
/// the last received PresentationPlan maps onto one of three tiers — deflated
/// (low arousal/valence), calm (mid), energetic (high) — each picking one of
/// the model's three idle motions.
enum IdleTier { deflated, calm, energetic }

/// Client-side idle picker: re-picks the idle motion every [repickInterval],
/// but only switches tier when the previous switch is older than
/// [switchLock], unless the affect crosses the tier threshold by a
/// [hysteresisMargin] (a clear mood swing switches immediately).
class IdleTierPicker {
  IdleTierPicker({
    this.repickInterval = const Duration(seconds: 30),
    this.switchLock = const Duration(seconds: 60),
    this.hysteresisMargin = 0.1,
  });

  /// How often the idle motion is re-picked.
  final Duration repickInterval;

  /// Minimum time between two tier switches.
  final Duration switchLock;

  /// Extra distance beyond a threshold that allows an immediate switch.
  final double hysteresisMargin;

  double? _valence;
  double? _arousal;
  DateTime? _lastPick;
  DateTime? _lastSwitch;
  IdleTier _tier = IdleTier.calm;

  IdleTier get tier => _tier;

  /// Affect thresholds (arousal in [0,1], valence in [-1,1]; baselines follow
  /// the backend AffectState defaults in relationship/affect.go).
  static IdleTier tierFor({required double valence, required double arousal}) {
    if (arousal <= 0.15 || valence <= -0.35) return IdleTier.deflated;
    if (arousal >= 0.55 && valence >= 0.15) return IdleTier.energetic;
    return IdleTier.calm;
  }

  /// Idle motion of the model (idle_01-03 in the model3.json) per tier.
  static String motionFor(IdleTier tier) {
    switch (tier) {
      case IdleTier.deflated:
        return 'idle_01';
      case IdleTier.calm:
        return 'idle_02';
      case IdleTier.energetic:
        return 'idle_03';
    }
  }

  /// Feeds the affect vector of the latest PresentationPlan.
  void updateAffect({double? valence, double? arousal}) {
    if (valence != null) _valence = valence;
    if (arousal != null) _arousal = arousal;
  }

  /// Returns the idle motion to play when a re-pick is due, otherwise null.
  String? maybeRepick(DateTime now) {
    final last = _lastPick;
    final first = last == null;
    if (!first && now.difference(last) < repickInterval) return null;
    _lastPick = now;
    final valence = _valence ?? 0;
    final arousal = _arousal ?? 0.2;
    final desired = tierFor(valence: valence, arousal: arousal);
    if (desired != _tier) {
      final lastSwitch = _lastSwitch;
      final lockExpired =
          lastSwitch == null || now.difference(lastSwitch) >= switchLock;
      // A fresh idle session never lands on energetic: the baseline pick is
      // calm or deflated (starting hyped reads as a glitch), and energetic is
      // earned after the baseline has been established.
      final baselinePick = first && desired == IdleTier.energetic;
      if (!baselinePick &&
          (lockExpired || _crossesThresholdByMargin(desired, valence, arousal))) {
        _tier = desired;
        _lastSwitch = now;
      }
    }
    return motionFor(_tier);
  }

  /// True when the affect is beyond the desired tier's thresholds including
  /// the hysteresis margin, justifying an immediate tier switch.
  bool _crossesThresholdByMargin(IdleTier desired, double valence, double arousal) {
    final m = hysteresisMargin;
    switch (desired) {
      case IdleTier.deflated:
        return arousal <= 0.15 - m || valence <= -0.35 - m;
      case IdleTier.energetic:
        return arousal >= 0.55 + m && valence >= 0.15 + m;
      case IdleTier.calm:
        return arousal > 0.15 + m &&
            arousal < 0.55 - m &&
            valence > -0.35 + m &&
            valence < 0.15 - m;
    }
  }
}
