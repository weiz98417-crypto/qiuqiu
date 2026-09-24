# Tasks: Delivery Outcome Uplink

- [x] 2.1 下发音频消息携带 deliveryKey（已有，watchconnection 下行 eventKey 直通）；客户端 audio_player_native 播放终态事件（播完/pause 顶替/静音跳过）→ `playback_result {deliveryKey, state, reason}` 上行（AudioState/playEncoded/PlaybackSessionEvent 全链透传 deliveryKey；ended→completed、interrupted→interrupted、blocked→skipped、failed→failed；缺键旧路径不发）。
- [x] 2.2 服务端 WS 上行分发新增 playback_result 处理：FindByDeliveryKey 推进七态（Source=client）；推断路径降级为兜底（连接级 clientPlaybackReportSet 登记，实报在先则 observeInferredOutcome 跳过，账本事件标 Source=server_inferred）；迟到/未知记录记 client_late 不覆盖；owner 不符拒写。
- [x] 2.3 确定性用例落在 cmd/server 单测四例（实报 completed 路由 / 迟到不覆盖 / 未知键 client_late / 实报后推断跳过）——evals/cases JSON 形态不可行：Go 评测 runner 无 WS 会话缝，Case schema 表达不了回执注入（design.md 的「脚本 WS 先例」实为 scripts/evals/runtime-e2e.mjs）；如需 JSON 形态须先扩 runner 缝，超出本卡范围。客户端侧 audio_protocol_test 3 例（带键/缺键实报、静音跳过、映射表）。
- [x] 2.4 卫生：eval-case.schema.json 同步 12 目录（suite.enum + id.pattern 双处）+ 全量门禁。
  - 门禁记录：go test 28 包全绿、dart analyze 无新增、flutter test 36 例过；pr tier 两次全量 87 passed/1 failed 且失败用例轮转（p95 性能与 fulltime-farewell 各一次）——本机时序抖动（fulltime 有 bisect 前科），与波 2 改动无逻辑关联，如实记录。

## Sequencing

六卡实施波 2。voice-duplex 1.1–1.4 的前置通道；完成后跑 handoff 全量交互电池作回归基线。
