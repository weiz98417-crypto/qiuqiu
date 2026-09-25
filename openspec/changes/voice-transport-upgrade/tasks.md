# Tasks: Voice Transport Upgrade

- [ ] 1.1 端到端延迟分解采集（采集/上行/ASR/决策/TTS 首包/播放起）+ 弱网模拟。（2026-09-25：服务端 WS 链路结构化延迟日志点与单测锁已落，见 design.md；真机 p90 采集与弱网模拟待真机会话，与 voice-turn-detection 数据采集并批）
- [ ] 1.2 决策门评估：WS 增强 vs WebRTC（含本地部署 STUN/TURN 最小配置成本），决策+数据写回 design.md。
- [ ] 1.3 实施所选路径（WS 增强：分片/优先级/心跳；或 WebRTC 评估迁移）。
- [ ] 1.4 evals/压测：延迟目标达成验证（对接阶段 F 90 分钟压测留尾）。
- [ ] 1.5 验证：全量门禁绿。

## Sequencing

I 系列第 3 个（末位），依赖 voice-duplex + voice-turn-detection 的实测数据。顺手清偿「阶段 F 90 分钟压测」留尾。
