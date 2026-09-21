# Design: Memory Into Turns

## 保守门（本 change 的零漂移关键）

`realizeWithMemory` 的开火条件：`a.realizer != nil && decision.Speech != nil && recall 材料非空`。三个调用面（主动回合、线程回访、事实补充语）全部过这道门——无记忆材料（新用户、未配 seam、adapter 降级、evals）时原文路径，行为与改动前逐字一致。

## 主动回合

- 措辞目标 = 运营 ProactiveText（或罐头 fallback），身份是**锚点**：喂 reliable、进 guard 锚源；realizer 只在记忆材料允许时围绕它重措辞。
- recall focus 取 `PlayerName`（空则 `TeamName`）：recall 是双向 contains 语义，整段事件描述永远匹配不上；单实体词才能命中「用户记忆里提过这个球员」。
- 触发后 `trace.Reason = proactive_memory_realized`，emit ToolCall 标 `mode=realized, source=match_event_memory`。
- 运营手注事件（decision.ID == ""，无 Speech）保持原文——手写的字原样出去是导演台契约。

## 开线程回访

- 合成最小 realize 决策载体（`threadRecoveryDecision`，ActRecall 语义、≤3 句），只作 RealizationRequest 的请求载体，不进 director 存储。
- trace 构建顺序调整：先建 trace（带 recover_thread），再尝试重措辞，emit 模式如实标记。

## 事实应答补充语

- 插入点：意图 handler 之后、fact_language 检查与 recentPhraseHashes 之前——补充语随最终 reply 进 applyDecision 与账本。
- `appendFactMemoryCallback`：仅 fact 意图 + recall 非空；衔接语单独过 guard（`factCallbackDecision`：一句为限、允许一个问句、BanterScope=none），guard 的锚源放宽到 `reply + memoryContext`（衔接语里的球员/球队来自 recall 是合法来源）；拒绝即整句丢弃。
- 事实本体措辞归确定性路径所有（ADR-0009 design decision 4 不动摇）：追加的是句子，不改写事实句。

## 独立任务 A（配置）

`deploy/memobase/config.yaml` 打开 `enable_event_embedding`，embedding provider 指向 OpenAI 兼容端点（`MEMOBASE_EMBEDDING_*` 环境变量进 docker-compose）；key 未配置时 Memobase 自身降级，本地 contains 召回照旧。存量记忆无向量、仅新写入生效，实施时验证回填能力并如实记录。

## 删除测试

- `memory_realization.go` 若删除：记忆再次只剩闲聊窄口，主动回合/回访退回罐头——复杂度（记忆如何影响言行）收进这一个 module，是加深不是新增层。
