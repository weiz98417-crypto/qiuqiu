# Voice Transport Upgrade: 传输层评估与升级

## Why

语音现走 WS（asr_chunk 上行/transcript 下行/TTS 音频下发）。全双工（voice-duplex）与轮次检测（voice-turn-detection）落地后，WS 音频的延迟/抖动/丢包是否仍达标需要数据说话——行业同位产品普遍走 WebRTC（LiveKit/pipecat/TEN），但引入 WebRTC=新传输栈+STUN/TURN 运维，对本地部署是小团队真实成本。**决策而非默认**：用实测数据决定升级或增强，避免为不存在的延迟问题引入运维负担。

## What Changes

- **实测基线**：voice-duplex/turn-detection 落地后，采集端到端延迟分解（采集→上行→ASR→决策→TTS 首包→播放起）与弱网表现（本地可模拟限速）。
- **决策门**：抢话响应 p90 ≤ 800ms 且弱网可用 → WS 增强（分片/优先级/心跳收紧）即止；超标 → WebRTC 评估（datachannel 保留 WS 信令 vs 全迁移；STUN/TURN 对本地部署的最小配置）。
- 实施所选路径；决策与数据记录进本 change design（含否决路径的理由）。

## User Stories

1. As a 用户, I want 弱网下抢话依然秒回, so that 通话感不断。

## Non-goals

- 电话/SIP 接入；多人群聊传输；S2S 模型流式协议。

## Success Criteria

- 延迟分解数据落档；决策有数据依据；若升级：WebRTC 链路全量门禁绿；若不升级：WS 增强达成延迟目标且理由可查。
