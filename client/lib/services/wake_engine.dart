/// 唤醒 KWS 引擎的 conditional import 缝（wake-word-kws 10.2，照
/// recorder_stub.dart 先例）：默认（native/flutter test）走 sherpa 实现，
/// web 走 stub——web 排除唤醒形态（页面失焦麦克风即停），但构建必须不坏
/// （9b6e1f0 教训）。
library;

export 'wake_engine_sherpa.dart' if (dart.library.html) 'wake_engine_stub.dart';
