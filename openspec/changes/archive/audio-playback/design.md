# Audio Playback 技术设计

## 1. 架构

```
WebSocket 二进制帧 (PCM 16bit 24kHz mono)
    │
    ▼
Jitter Buffer (Ring Buffer, 200ms)
    │
    ▼
AudioPlayer (flutter_soloud 或 audioplayers)
    │
    ▼
系统音频输出
    │
    ▼ (回调)
Live2D Widget: setSpeaking(true/false)
```

## 2. 技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| 音频播放 | `flutter_soloud` | 低延迟，支持 PCM 流式推送，C++后端 |
| 备选 | `audioplayers` | 更成熟但延迟略高 |

## 3. Jitter Buffer

```dart
class JitterBuffer {
  final int bufferDurationMs = 200;
  final List<Uint8List> _buffer = [];
  Timer? _drainTimer;

  void push(Uint8List pcmFrame) {
    _buffer.add(pcmFrame);
    if (_buffer.length >= bufferSize) {
      _drainTimer ??= Timer.periodic(Duration(milliseconds: 20), (_) => drain());
    }
  }

  Uint8List? drain() {
    if (_buffer.isEmpty) return null;
    return _buffer.removeAt(0);
  }
}
```

## 4. 音频焦点管理

| 场景 | 行为 |
|------|------|
| 来电 | 暂停播放，Live2D 切换 idle |
| 挂断 | 恢复播放 |
| 切后台 | 继续播放（启用后台音频 capability） |
| 其他 App 播放 | 降低音量或暂停 |
| 用户手动静音 | 停止播放，LLM/TTS 继续但音频丢弃 |

使用 Flutter `audio_session` 插件管理焦点。

## 5. 播放状态反馈

```dart
// 播放开始时
live2dWidget.setSpeaking(true);

// 播放结束时
live2dWidget.setSpeaking(false);
```

状态切换通过 `ValueNotifier<bool>` 驱动，避免不必要的 WebView evaluateJavascript 调用。

## 6. 性能目标

| 指标 | 目标 |
|------|------|
| 音频延迟 | < 50ms（从收到首帧到听到声音） |
| 播放卡顿 | 90 分钟播放零 audible glitch |
| 后台播放 | 持续 2h（一场完整比赛） |
| 内存 | 播放期间 < 20MB 额外内存 |
