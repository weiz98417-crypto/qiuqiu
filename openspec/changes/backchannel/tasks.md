# Tasks: Backchannel

- [x] 3.1 internal/backchannel：决策器（白名单/限频/短语池）+ ADR-0016。
- [x] 3.2 watchconnection 事件泵挂微反应直发（绕回合调度）+ trace/Ledger 审计。
- [x] 3.3 单测：限频（半场/全场/quiet/手动占用）、白名单、短语轮转。
- [x] 3.4 验证：全量 go test + eval 绿（事件流零污染）。

## Sequencing

第二波第 5 个（放最后：表情表现力受 live2d-motion-pack 资产进度影响，v1 用现有槽位先行）。

## 触发型留尾（Q3 标准格式）

- 触发：v1 文字气泡验证有陪伴价值 + 客户端排期；动作：v1.1 音频（客户端音频队列 FIFO→deliveryKey 配对 + 短 TTS 接入）。
- 触发：v1.1 稳定且出现流式需求；动作：SSE 流式语音（从 openaicompat 重建，真消费者=backchannel/长回复）。
