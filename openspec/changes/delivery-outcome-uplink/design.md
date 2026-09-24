# Design: Delivery Outcome Uplink

## 形状

上行消息（客户端→服务端，与既有 asr_chunk/interrupt/reply_displayed 同通道）：

```
{"type":"playback_result","deliveryKey":"...","state":"completed|interrupted|skipped","reason":"..."}
```

- 服务端 handler 挂 watchconnection 上行分发（与 reply_displayed 并列，不动它）。
- deliveryKey 生成方不变（服务端下发时已带投递键语义，无则在本 change 补下发字段）；客户端只透传，不理解语义。

## 兜底与迟到

- 兜底窗口：服务端推断改为「下发后 N 秒无回执才写」，N 可配，默认对齐客户端播完上报的自然上限（句长相关，实施时以现有句柄轮询周期 100ms×典型句时长标定）。
- 迟到回执：回合已终态后到达→仍记账，Source=client_late，不回改已写条目（账本 append-only 纪律）。

## Source 语义

账本 Event.Source 增取值：client（实时实报）/ client_late（迟到实报）/ server_inferred（兜底推断，即现状权威值降级后的名字）。若 Source 字段是自由文本则直接写，受枚举约束则在 Reason 前缀区分——实施时按 interaction/ledger.go 现状定，倾向前者。

## 测试面

evals 确定性用例走「假客户端 WS 会话」缝（evals 已有脚本 WS 实现先例）：注入回执/不回执/迟回执，断言账本三态路由。客户端侧 flutter test 断言播放器终态→上行消息映射。
