import 'dart:typed_data';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:qiuqiu/services/vad_service.dart';

/// 红测（bug 猎手）：_processAudio 里「音频缓冲/下发」分支块出现了两份
/// （vad_service.dart 同一函数内两段完全相同的 if/else），疑似坏合并。
/// 每帧 PCM 会被加进 _audioBuffer 两次、经 audioChunks 下发两次：
/// pushToTalk 的整段录音字节翻倍、freeTalk 的 ASR 流每个分片重复。
/// 本测试锁「一帧只缓冲/下发一次」，在坏合并存在时必红。
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('pushToTalk 每帧音频只缓冲与下发一次', () async {
    _RecordPlatformStub();
    final vad = VADService();
    addTearDown(vad.dispose);

    var chunkEvents = 0;
    vad.audioChunks.listen((_) => chunkEvents++);

    await vad.startListening(VADMode.pushToTalk);
    vad.onAudioData(_pcmFrame(0.03));
    vad.onAudioData(_pcmFrame(0.03));
    await Future<void>.delayed(Duration.zero);

    expect(chunkEvents, 2,
        reason: '两帧采集音频必须恰好下发 2 个分片（当前坏合并下发 4 个）');
    final drained = vad.drainAudio();
    expect(drained, isNotNull);
    expect(drained!.length, 3200,
        reason: '两帧×1600 字节：缓冲区不得每帧翻倍（当前坏合并 6400 字节）');
  });
}

/// record 插件平台通道桩（与 vad_service_test 同款）：驱动真实 VADService
/// 时把平台调用拦在测试进程内，权限恒通过、起流/停流为空操作。
class _RecordPlatformStub {
  _RecordPlatformStub() {
    _messenger.setMockMethodCallHandler(
      const MethodChannel('com.llfbandit.record/messages'),
      _onMethodCall,
    );
  }

  TestDefaultBinaryMessenger get _messenger =>
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;

  Future<Object?> _onMethodCall(MethodCall call) async {
    if (call.method == 'create') {
      final recorderId = call.arguments['recorderId'] as String;
      for (final prefix in const ['events', 'eventsRecord']) {
        _messenger.setMockMethodCallHandler(
          MethodChannel('com.llfbandit.record/$prefix/$recorderId'),
          (_) async => null,
        );
      }
    }
    if (call.method == 'hasPermission') return true;
    return null;
  }
}

/// 50ms@16kHz 单帧 PCM（1600 字节）。
Uint8List _pcmFrame(double rms) {
  final amplitude = ((rms.clamp(0.0, 1.0)) * 32767).round();
  final bytes = Uint8List(1600);
  final view = ByteData.view(bytes.buffer);
  for (var offset = 0; offset < bytes.length; offset += 2) {
    view.setInt16(offset, amplitude, Endian.little);
  }
  return bytes;
}
