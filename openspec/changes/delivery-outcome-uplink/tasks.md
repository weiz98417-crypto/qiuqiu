# Tasks: Delivery Outcome Uplink

- [ ] 2.1 下发音频消息携带 deliveryKey；客户端 audio_player_native 播放终态事件（播完/pause 顶替/静音跳过）→ `playback_result {deliveryKey, state, reason}` 上行。
- [ ] 2.2 服务端 WS 上行分发新增 playback_result 处理：写投递账本（Source=client）；推断路径加超时窗口降为兜底（Source=server_inferred）；迟到回执记 client_late 不覆盖。
- [ ] 2.3 evals：实报/无回执兜底/迟到回执三态路由用例（确定性，pr tier）。
- [ ] 2.4 卫生：eval-case.schema.json 同步 12 目录（suite.enum + id.pattern 双处）+ 全量门禁绿。

## Sequencing

六卡实施波 2。voice-duplex 1.1–1.4 的前置通道；完成后跑 handoff 全量交互电池作回归基线。
