# 0006 · 记忆 seam：领域接口 + Memobase 适配器 + 异步写入

## 背景

球球的记忆是流水账：`conversation.read_recent` 只取最近窗口，无重要性评分、无相关性检索、无综合反思——「老球友」的连续感没有地基。业界基准（Stanford 生成式智能体的记忆流三因子检索与反思、Neuro-sama 的跨场连续性、小冰的长短期记忆分工）表明「活着感」的地基是记忆综合能力。同时，开源记忆层（Mem0 / Memobase / Letta / Graphiti）已把事实提取、冲突裁决、画像管理做成成熟轮子，不必重造。

## 决定

1. **分工制**：Interaction Ledger 保持本地、append-only、可重放——它是事实源；Memobase 只承载**可变的综合记忆**（用户画像、事件摘要、反思洞察），每条综合记忆必须引用 Ledger 序号作为出处。延续「Redis 不是事实源」的既有哲学。
2. **领域接口**：记忆以球球自己的概念定义 seam——`Memories{Observe, Recall, Portrait, Threads}`（backend/internal/memory），Memobase 是第一个 adapter，内存 fake 是第二个（测试用）。轮子可换，seam 不动。
3. **异步写入**：回合内只写 Ledger（毫秒级）；观察入队后台刷入 Memobase，重试带退避。陪看回合的延迟不受记忆提取影响。每次提取的接受/拒绝决策带 reason code 本地留痕——事实优先文化同样约束记忆。
4. **降级**：Memobase 不可达时本地积压、检索回退 `read_recent`，对用户不可见。

## 被否决的替代方案

- **Memobase 全量接管记忆**：综合记忆可变（合并/改写/删除），让它碰事实流水等于放弃可审计与重放能力。
- **Letta/MemGPT 整框架**：它是完整的 agent 运行时，与球球已有的 Go 决策内核（策略表、调度器、realize）重复冲突。
- **Graphiti/Zep**：时序知识图谱能力强，但需引入 Neo4j，且 Zep 开源策略近期变动，依赖风险高。
- **纯 Go 自研记忆**：可行（pgvector 已在 compose 中）但重写提取/冲突裁决；若 Memobase 不满足，可作为第二个 adapter 替换——这正是定义 seam 的目的。

## 后果

- docker-compose 新增 memobase-server；写路径引入一次 LLM 提取调用（异步、有费用）。
- 「老球友」连续性成为可实现目标：Recall 供 callbacks，Portrait 供 C3，Open Thread 台账供主动回合理由（C2）。
- 记忆综合产物永不创建、更正或断言 Match Facts（ForbiddenClaims 纪律不变）。
