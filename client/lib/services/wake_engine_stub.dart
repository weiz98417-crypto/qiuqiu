import 'wake_service.dart';

/// web/不支持平台的替身实现（wake-word-kws 范围：web 排除、iOS 留配置位）。
/// 工厂返回不可用引擎，WakeService.setEnabled 会把它归入 failure 态；
/// 平台门 [isWakePlatformSupported] 恒 false，接线层据此不开唤醒链路。

bool get isWakePlatformSupported => false;

Future<KwsEngine?> createDefaultKwsEngine(WakeConfig config) async =>
    // 工厂抛错由 WakeService 归入 failure 态（平台不支持）。
    throw UnsupportedError('wake word KWS is not available on this platform');
