# Voice Duplex: 播放期收音与抢话打断

## Why

流式转写与 interrupt 通道都已存在（原生+web 的 VAD→`asr_chunk`→`asr.StreamSession`→`transcript_partial`；`{'type':'interrupt'}` WS 指令），但语音是「说完→想→播」的半双工节奏：球球说话时收音暂停（或收音不被处理），用户抢话（「哇这球！」）进不来——观赛抢话是高频共情场景，错过即出戏。架构评审 I 项用户裁决：全双工一步到位、spec 拆好串行。

## What Changes

- **现状盘点（先行任务）**：实测播放期 VAD/收音/ASR 会话的真实状态（原生与 web 分别盘），产出差距清单再动手——防止重复造已有的东西。
- **播放期收音**：TTS 播放中 VAD 保持开启、ASR 会话保持活（或轻量重建），用户语音照常走 `asr_chunk`。
- **说话抢断**：播放期识别到用户语音（置信门）→ 停 TTS 播放 + 发 `interrupt` + 用户话轮照常进入决策——「球球被抢话」成为一等交互。
- **回声规避**：球球自己的 TTS 被麦克风拾取导致自打断。策略分层：①客户端播放期 VAD 置信门提高+最短语音时长门；②播放期 ASR 首选偏保守（可配置关闭播放期识别=降级半双工）；③无法根除时记录事件供 I-2 轮次检测接管。参照 Open-LLM-VTuber 的「AI 听不到自己说话」工程实践。

## User Stories

1. As a 用户, I want 球球说话时我插一句「慢点这球啥情况」, so that 像真朋友聊天不用等她说完。

## Non-goals

- 语义轮次检测（I-2 voice-turn-detection）；传输层更换（I-3）；ASR/TTS 供应商变更。

## Success Criteria

- evals H 域相关例：播放期用户语音→TTS 停止+interrupt 下发+用户话轮被处理；回声自打断负例（播放自身不触发）；降级开关可用；全量门禁绿。
