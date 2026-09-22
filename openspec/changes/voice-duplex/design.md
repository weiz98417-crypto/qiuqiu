# Design: Voice Duplex

## 抢断判定（v1 纯 VAD，模型留 I-2）

```
TTS 播放中:
  VAD speech_start（置信度 > 播放期阈值 playConfGate，默认高于空闲期 1.5×）
  且 语音时长 ≥ 400ms（防喷麦/环境音）
  → stopPlayback() → sendWS('interrupt') → 用户话轮照常（transcript_final→agent）
```

播放期阈值与最短时长进 client config，可整体关闭（`duplex_playback_capture=off` → 行为=现状半双工）。

## 回声规避三层

1. **门限**：播放期 VAD 置信门抬高+时长门（上表）。
2. **频率错开**：客户端在 TTS 播放起止给 VAD 发 squash 窗（播放开始后 300ms 内忽略 speech_start——爆破音/耳机漏音主区）。
3. **兜底**：检测到「疑似自打断」（interrupt 后 transcript 为空/极短且与刚播文本重叠）→ 记 telemetry 事件，恢复播放；连续 N 次自动降级半双工并提示。

## 服务端

`interrupt` 通道现状已支持（match_session_controller.interrupt → server interrupted）。需要补的：播放期到达的 `asr_chunk` 在服务端 transcription 会话的接纳（若现状播放期拒绝/暂停会话则放开）；中断时进行中话轮的取消语义复用现有 interrupted 路径。

## 平台差异

原生（record 流）与 web（getUserMedia）的 VAD 置信度基线不同——播放期阈值分平台默认值，实施时各校准一轮（用 H 域 eval 固化）。
