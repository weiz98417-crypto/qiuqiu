# Design: Voice Transport Upgrade — 延迟分解日志点（2026-09-25 · 数据采集部分）

本波只交付端到端延迟分解的**埋点**与锁（与 voice-turn-detection 决策的数据采集并批，见其 proposal 修订注）；决策门评估（WS 增强 vs WebRTC）与真机 p90 采集**待真机会话**，不在本轮硬造。

## 结构化延迟日志点（对齐 duplex_event 风格，不建新系统）

统一格式（watchconnection.go `logVoiceLatency`）：

```
voice latency event: user=%q match=%q signal=%q stage=%q elapsed_ms=%d
```

| # | stage | 位置 | 语义 | elapsed 基准 |
| --- | --- | --- | --- | --- |
| 1 | `speech_received` | `user_speech` 消息处理 | 非流式话轮到达 | 到达时刻（elapsed_ms=0，并落锚点） |
| 2 | `speech_received` | `asr_start` 消息处理 | 流式转写会话开始（等价到达） | 同上；另落 `utt:<utteranceId>` 锚点 |
| 3 | `asr_finish` | `asr_finish` 消息处理 | 客户端说完信号到达 | `utt:` 锚点（asr_start） |
| 4 | `asr_final` | 转写 `transcript_final` 下行回调 | ASR 终稿产出 | `utt:` 锚点（真实 ASR 耗时） |
| 5 | `turn_decided` | `submitUserTurn` 话轮体 | 话轮决策完成（含事实刷新） | signal 锚点 |
| 6 | `audio_delivered` | `submitUserTurn` 投递成功后 | 响应投递完成；WS 路径的 TTS 合成与音频下行都在投递服务内，此行覆盖到下行完成 | signal 锚点 |
| 7 | `tts_synthesized` | `completeVoiceSessionWithOptions`（操作台 HTTP 语音路径） | TTS 合成完成 | 该次话轮 `now` 入参 |

- 锚点表：连接内 `voiceLatencyAnchors`（signalID / `utt:<utteranceId>` 两类键，FIFO 上限 64，只影响迟到消息的日志行）。
- 覆盖任务 1.1 要求的五段：user_speech 到达（1/2）→ ASR finish（3/4）→ turn 决策（5）→ TTS 合成调用（7，操作台路径；WS 路径并入 6）→ 音频下行（6）。
- 端到端分解的前两段（采集→上行）在客户端，随真机会话从同一日志行 + 客户端时间戳对齐采集。

## 单测锁（handler 层面边界）

`backend/cmd/server/voice_latency_test.go`：

- 日志行字段完整（user/match/signal/stage/elapsed_ms 五字段齐备）。
- 锚点缺失（零值）跳过日志行，不出噪音行；锚点表 FIFO 淘汰。
- `asr_start`/`asr_finish` handler 走真实 `readMessages` 打出 `speech_received`/`asr_finish`。
- `user_speech` 全链路（真实调度器+伴答 agent+mock TTS）打出 `speech_received` → `turn_decided` → `audio_delivered`。
- 操作台语音路径打出 `tts_synthesized`。

## 待真机会话（不硬造）

- 真机 p90：抢话响应（speech_received → audio_delivered）与端到端（采集→播放起）按上述日志行采集，数据落回本文档后开决策门（p90 ≤ 800ms 且弱网可用 → WS 增强；超标 → WebRTC 评估）。
- 弱网模拟与 90 分钟压测留尾同批执行。
