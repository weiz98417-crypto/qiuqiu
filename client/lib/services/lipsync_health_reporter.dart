/// 嘴型退化上报限频器（快修 P1：wLipSync→包络→随机抖动的静默降级此前
/// 不可见）。JS 侧的退化事件是状态沿，但 webview 重载 / 重进比赛页会重复
/// 触发——同一原因在冷却窗口内只上报一次。
class LipSyncHealthReporter {
  LipSyncHealthReporter({
    this.cooldown = const Duration(minutes: 10),
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now;

  final Duration cooldown;
  final DateTime Function() _now;
  final Map<String, DateTime> _lastSent = {};

  /// 该原因是否应该现在上报（调用即记账，连续调用不会通过）。
  bool shouldReport(String reason) {
    final at = _now();
    final last = _lastSent[reason];
    if (last != null && at.difference(last) < cooldown) {
      return false;
    }
    _lastSent[reason] = at;
    return true;
  }
}
