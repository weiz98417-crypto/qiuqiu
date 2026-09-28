# Tasks: Operations Turn Replay

- [x] 1.1 `TraceStore.AttachVoiceStages`（内存+PG 双 adapter，JSONB 合并、幂等）；单测：后到覆盖同键、缺 trace 不报错仅计数。
- [x] 1.2 WS 路径改造：`logVoiceLatency` 四调用点（speech_received/asr_final/turn_decided/audio_delivered）双写 trace（attach），失败不反伤主链路；日志保留过渡期。
- [x] 1.3 HTTP 路径：`tts_synthesized` 构造内合并进 `result.Trace.Voice`（ensureVoiceMeta 既有模式）。
- [x] 1.4 `turnDecision` attach：turn_relay 结论（isComplete/source/queryLatencyMs）按 signalID→trace 关联落库；话轮无 trace（纯文本模式）时静默丢弃。
- [x] 1.5 console 契约：client.ts 类型补 `latencyStages`/`turnDecision`/`toolCalls` 渲染面；过 check-console-transport。
- [x] 1.6 `<TurnReplay trace>` 组件 + trace 夹具单测（span 顺序/失败段/缺 stage 容错）。
- [x] 1.7 显形四落点：Match 打断 ring 卡（ring 为全局容量 20，前端按 matchId 过滤）、Overview backchannel 计数卡（**overview 端点响应需加 backchannelToday/whitelistEventsToday 两键，golden 随之扩**）、User KIND_LABELS+phrase、CitationAudit toolCalls 列。
- [x] 1.8 `useConsoleQuery` + `<ObservationPage>` + 六页迁移 + patchThread 收敛；组件/hooks 单测。
- [x] 1.9 验证：golden 扩新键（键集断言非具体毫秒）、mock server 扩、pr tier 绿、`console && npm run build`、go 全量。

## Sequencing

运营端三波之首（波 1）；与 operations-metrics-stack 并行不冲突（面板②依赖本 change 的 latencyStages 落地）。

## 实施注记（2026-09-28）

- 1.6/1.8 的「单测」按 console 无测试框架的现状降级为 e2e 断言覆盖（console-pages 真后端下 TurnReplay 渲染 + 未连接态/占位态），组件单测基建本身留尾（引入 vitest 是独立决策）。

- User 页 TurnReplay 内嵌与直播右栏 trace 回放视图 scope 收缩：v1 回放只内嵌 CitationAudit 抽屉（唯一天然持 trace 的入口），User 页保持 KIND_LABELS+phrase；右栏跳比赛页。触发留尾见下。
- TurnReplay 补 playbackStatus/路由段也后置（数据在 wire 上，纯 UI 增量，与音频回放同批做）。
- golden 只扩 overview backchannel 三键；latencyStages/turnDecision 的键集断言由 attach 单测（companion + cmd/server）覆盖，PG 路径由集成测试覆盖（本机无 pgvector，部署轮生效）。

## 触发型留尾（Q3 标准格式）

- 触发：真机轮人耳确认 TTS 需要音频样本对照；动作：音频回放嵌片段（样本库+保留策略，隐私面单独评审）。
- 触发：延迟面板/回放发现 stage 粒度不够（如 ASR 流式分片耗时）；动作：扩 stage 集合（沿用 attach 通道，零 migration）。
- 触发：User 页需要单轮回放、或直播右栏需要就地回放；动作：TurnReplay 复用（组件已就绪，补 trace 取数接线）。
- 触发：并发 attach 丢更新窗口成为实际问题；动作：PG 侧改 `voice = voice || ?::jsonb` 原子合并（现实现为读改写，同连接顺序写不触发）。
