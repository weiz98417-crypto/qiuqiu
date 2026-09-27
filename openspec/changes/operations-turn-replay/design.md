# Design: Operations Turn Replay

## latencyStages/turnDecision 的落库时序（双模式，实施前必读）

两条语音路径的 trace 生命周期不同，合并方式必须分开（2026-09-28 代码实证）：

- **WS 路径（watchconnection.go）**：trace 在 agent 内部已 `WriteTrace` 落库**之后**，`turn_decided`（:733）与 `audio_delivered`（:785）才打点——构造时合并写不进去。必须走**事后 attach**：`TraceStore` 接口加 `AttachVoiceStages(ctx, traceID, stages map[string]int, turnDecision *TurnDecision)`（内存 adapter 改 map 内对象；PG adapter `UPDATE agent_traces SET voice = voice || ?::jsonb WHERE id = ?` JSONB 合并）。`logVoiceLatency` 的对偶改造：打点处调用 attach，失败仅计数不打断主链路（观测是旁路，永不反伤投递）。
- **HTTP 语音路径（main.go:935-960）**：`tts_synthesized` 在 `result.Trace` 构造中（`ensureVoiceMeta` 既有模式）——**构造内直接合并**，不 attach。
- `turnDecision` 关联链：turn_query 提前问发生在话轮进行中，结论由 `signalID → 话轮 trace` 关联；话轮 trace 落库前后结论都可能到达（模型回得早），统一走 attach 通道幂等合并（后到覆盖同键）。

## stage 语义

- 值 = 相对 `speech_received` 锚点的**累计**毫秒（不是相邻差）——面板堆积图与回放时间轴直接消费，减法留给展示层。**唯一例外**：`tts_synthesized` 是合成段耗时（相邻差）——WS 路径合成发生在投递服务内部，只有段耗时能廉价带回（ResponseDeliveryResult.SynthesisMS）；面板展示按「合成段」解读，不与其他累计段并读。
- 五枚 stage 名沿用日志现有命名，与 voice-transport-upgrade 的 design.md「延迟分解」节对齐：`speech_received`(0)/`asr_final`/`turn_decided`/`tts_synthesized`/`audio_delivered`。
- 锚点缺失（非本连接发起的话轮）该 stage 不写——与现有日志跳过语义一致，避免无基准噪音。
- WS 路径两次 attach（turn_decided 定格前三段；audio_delivered 只补 audio/tts 键）——第二次重算 turn_decided 会把它膨胀成全链时长（code-review 抓到并已修）。

## TurnReplay 组件

- 数据源：单条 trace 全量自足——`voice.latencyStages`/`voice.turnDecision`/`voice.*Status`（**含 playbackStatus：/traces 已把 ledger 投递终态回填进该字段**，match_operator_api.go:210-214）、`router`、`toolCalls`、`relationshipDecision`、`latencyMs`。无新端点、无跨接口拼装。
- 呈现：横向 span 条（Phoenix 瀑布风）——ASR(宽度=asr_final)→路由→RAG(toolCalls 展开)→决策→TTS(tts_synthesized/audio_delivered)→投递终态色块（playbackStatus）；失败段红色标注（asrError/ttsError/reason）。
- 隐私（ADR 待立，CONTEXT.md 已有词条）：转写正文仅在既有两页（CitationAudit 抽屉/User 页）内嵌展示，权限沿用页面自身 scope；组件不单独做路由即不产生新入口。

## ObservationPage / useConsoleQuery（组合 hooks，非框架）

- `useConsoleQuery(fetcher)` 返回 `{data, loading, error, reload}`（收敛 useAsync 之上的三件套组合）；`<ObservationPage title actions onLoadError>` 只管壳（标题栏/刷新/错误 Alert/Table loading）；列定义留在页面 JSX——审计台页面差异大，声明式配置对象会养出自研 CRUD 框架（react-admin 负结论同理）。
- 迁移面：Overview/Match/Threads/User/CitationAudit/Operators 六页 + patchThread 收敛进 useConsoleQuery 的 mutation 辅助（actingId/messageApi.success/reload 三连）。
- 删除测试：删 useConsoleQuery → 各页退回手写三件套（复杂度回散=收敛价值）；删 TurnReplay → traceEvidence 回到「为什么说话」抽屉，无悬空依赖。

## 回归面

- golden 快照将出现新字段（`latencyStages` 数值不稳定——归一策略：golden 断言键集与非负性，不断言具体毫秒；或夹具固定锚点）。`check-console-transport.mjs` 转译 client.ts 单测照常过。console spec 的 mock server（support/console-server.mjs）补两新键。
- pr tier 门禁伺服 `console/dist`——改完必须 `cd console && npm run build`（教训同 client bundle）。
