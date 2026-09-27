# Operations Turn Replay: 会话观测显形 + console 前端脚手架深化

## Why

C 端六轮落地了语音双工、smart-turn、backchannel 音频、RAG——运营端大多看不见。走查实证（2026-09-28）：`/traces` 已携带 voice 状态与 LatencyMS 但 console 类型/页面从不渲染；延迟分解与轮次决策只进 `log.Printf`；`/delivery-interruptions` API 前端定义了调用却无页面消费；六页手写「Alert+刷新+Table」三件套、patchThread 两处逐字重复。差距的主因是 **UI 边缘丢弃数据**，不是后端缺数据。

## What Changes

- **后端一次搬运（零 migration）**：`agent_traces.voice` JSONB 增两键——
  - `latencyStages`：五 stage（`speech_received` 基准 / `asr_final` / `turn_decided` / `tts_synthesized` / `audio_delivered`），值为相对基准的累计毫秒；数据本来就在算（voiceLatencyAnchors / HTTP 路径 now 锚点），从日志搬运进 trace。**注意第五 stage `tts_synthesized` 在操作台 HTTP 语音路径**（main.go:953），不是 WS 路径。
  - `turnDecision`：`{isComplete, source(model/rule/silence), queryLatencyMs}`——turn_relay 结论随所属话轮 trace 落库，A 回放与真机 800ms 门校准吃同一份数据。
- **前端 `<TurnReplay trace>`**：traceEvidence 深模块扩展的 span 时间轴组件（ASR→意图路由→RAG 检索 toolCalls→关系决策→TTS 指令/耗时→投递终态），CitationAudit 抽屉与 User 页就地内嵌，**不设独立路由**（Q6-b）。投递终态直接用 `trace.voice.playbackStatus`——`/traces` 已把 ledger 终态回填进该字段（match_operator_api.go:210-214 实证），零新数据源。
- **显形四落点**：Match 页打断 ring 卡（API 已在，接消费）；Overview 增 backchannel 今日计数卡（发出数 vs 白名单事件数对比——`backchannel.Decide` 拒绝路径无痕，**「限频命中」无数据源**，口径定为发出/事件比）；User 页补 KIND_LABELS 的 backchannel 条目+phrase 列；CitationAudit trace 表补 toolCalls 列（RAG 命中/回退可见）。
- **脚手架深化（D）**：`useConsoleQuery`（取数/刷新/错误态）+ `<ObservationPage>` 壳组件，组合 hooks、页面保留 JSX 列定义；六个观测页迁移，Threads/User 的 patchThread 收敛为一处。
- 日志双写一个过渡期后退役（`logVoiceLatency` 调用点改为写 trace；日志保留至面板验证稳定）。

## User Stories

1. As a 审计员, I want 点开一轮对话看 span 时间轴回放, so that 「球球为什么抢话/为什么这句慢」不用翻服务器日志。
2. As a 导播, I want 本场比赛谁被打断、微反应发了什么, so that 人工注入节奏有依据。
3. As a 信任守门人, I want 回放显示转写正文时权限与用户详情页一致, so that 观测显形不扩大内容暴露面。

## Non-goals

- 音频片段回放（留尾：真机轮人耳需求触发样本库）；Grafana 指标面（波 2）；实时流（波 3）；新后端查询端点（复用既有 /traces 与 ledger 投影）。

## Success Criteria

- golden 扩 `latencyStages`/`turnDecision` 字段（时间戳归一机制现成）；TurnReplay/useConsoleQuery 单测（trace 夹具）；client.ts 过 check-console-transport；pr tier console spec 扩 mock 后绿；`console && npm run build` + go 全量绿。
