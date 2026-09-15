---
id: 47-fact-ledger-conflicts
title: 比赛事实账本与冲突调和
source_chapter: docs/球球全套资料/5.AI Coding工程实践/47-比赛事实账本与冲突调和.md
status_summary: { implemented: 13, partial: 0, planned: 0, concept: 1 }
---

# 比赛事实账本与冲突调和

本章管事实层的地基：追加式事件账本、从任意位置重放投影、互斥来源冲突成簇调和、幂等操作写与事务性发布。这是三章里兑现度最高的设计——旧文档第 1-8 节描述的账本/冲突/回放/幂等语义几乎逐条有对应实现；只有第 10 节的 make 命令族和独立微服务属于从未存在的工程想象（真实入口是 `backend/cmd/server` 单体）。

## 系统实际怎么工作

**账本是追加式事件表加场次级序号。** 每条事件落库时获得 `recorded_sequence`（022 迁移补序列并回填历史，implemented-at backend/migrations/022_fact_ledger_recorded_sequence.sql:1-12）。重放前的 `orderFactEvents` 拒绝重复序号，并要求要么全部事件带序号、要么全不带——半带序号的历史直接判非法（implemented-at backend/internal/matchstate/fact_ledger.go:279-302）。「只追加、不回写旧位置」因此有了可校验的形式。

**回放是一等公民。** `Replay(input, uptoSequence)` 把事件按序号过滤到指定账本位置后重放投影；`Project` 就是 `Replay(input, 0)`（implemented-at backend/internal/matchstate/fact_ledger.go:76-97）。投影输出携带 `ProjectorVersion = fact-ledger-projector/v1` 和 `ProjectedSequence`，并写入快照（implemented-at backend/internal/matchstate/fact_ledger.go:11, 248-251）——消费者能判断快照出自哪版规则、算到哪条事件。

**进球投影是显式六态机。** pending/active/cancelled/checkpointed/revoked/unavailable（implemented-at backend/internal/matchstate/fact_ledger.go:62-69）。公开进球按 FactID/ID 别名归一（歧义引用直接报错）；`goal_cancelled` 通过 `RevisionOf` 找到原进球、比分 -1、状态转 cancelled，且重复取消、取消从未公开的球、取消出现在进球之前都是硬错误（156-198）；`score_correction` 直接设定比分并把仍活跃的进球检查点化，防止旧取消事件在修正后错误回滚（199-209）。负比分在循环里兜底拒绝（211-213）。

**追加即验证，不只是存储。** `Create` 在写入前调用 `ValidateAppend`，把新事件放进「现有历史 + 候选」的投影里校验：进球比分必须恰好等于当前投影 +1，goal_cancelled 必须恰好 -1，其他事件类型不得改比分（implemented-at backend/internal/matchstate/store.go:679-682, 1486-1528）；score_correction 必须携带 correctionReason 证据，goal_cancelled/var_result 必须带 revisionOf（implemented-at backend/internal/matchstate/store.go:1445-1459）。

**冲突成簇，不选边。** 跨源候选若同事件类型、45 秒时钟窗内、队伍/球员互斥，新事件被标 conflict，快照 integrity 转 conflict（implemented-at backend/internal/matchstate/store.go:650-674）。簇结构是成员（accepted/candidate 角色）加无向边（reason=cross_source_fact_conflict），与已有开放簇相交时自动合并成一个簇（implemented-at backend/internal/matchstate/store.go:936-998）——这正是旧文档「防止同屏 1:0 与 1:1 打架」的落点。

**冲突图有数据库形态。** 三张表：fact_conflicts（open/resolved、chosen_fact_id、resolved_by）、fact_conflict_members（accepted/candidate 主键对）、fact_conflict_edges（left<right 有序边，外键必须指向成员）（implemented-at backend/migrations/023_fact_conflicts.sql:1-20, migrations/024_fact_conflict_graph.sql:1-13）；024 还把历史遗留的 accepted×candidate 组合回填成边（024:18-31），027 给裁决事实补齐 reason/resolved_by 审计（implemented-at backend/migrations/027_fact_conflict_resolution_audit.sql:1-10）。

**调和是图上的可行集选择。** `resolveConflictSelection` 把边当互斥约束：选中的事实里若含一条边的两端直接报 mutually exclusive；选中事实的边对手被 revoke；被选中且原为 conflict/provisional 的事实转 reconciled；不再与任何剩余边相邻的孤立 conflict 事实释放回 provisional；无剩余边时簇 resolved（implemented-at backend/internal/matchstate/conflict_resolution.go:51-168）。`ResolveConflict` 命令把这份转移应用到事件流：reconcile 置 confirmed、revoke 清 PublicAt、每处 FactRevision 自增（implemented-at backend/internal/matchstate/fact_commands.go:28-108）。对已 resolved 的簇再裁决直接报「already resolved」（conflict_resolution.go:52-54）——旧文档「只有一个有效裁决」是真实约束。

**操作写统一幂等。** 所有操作员写经 `executeOperatorWrite`：Idempotency-Key + 方法/路径/请求体的 payload 哈希，重复请求重放已存响应并带 `Idempotency-Replayed` 头；facts.confirm/revoke/reconcile 与 conflicts.resolve 标记为原子操作（implemented-at backend/cmd/server/operator_write_api.go:46-93）；幂等记录带 operation/response/status_code 列（implemented-at backend/migrations/017_operator_idempotency_outbox.sql:1-18）。旧的单选 chosenFactID 接口经 `CompatibleSelectionForLegacyChoice` 换算成兼容选边集，且强制要求显式理由（implemented-at backend/internal/matchstate/conflict_resolution.go:19-49, store.go:1107-1138）。

**发布走事务性 outbox。** 事件行与 outbox_messages 在同一事务写入，聚合键是 `eventID:revision:factStatus`（implemented-at backend/internal/matchstate/postgres.go:222-239）；`RunOutbox` 每 250ms 用 `FOR UPDATE SKIP LOCKED` 认领一条待发布消息，30 秒租约内重试，发布成功前下游看不到（implemented-at backend/internal/matchstate/postgres.go:110-178）。这兑现了旧文档「账本仍保留待传播状态，消费方恢复后收敛」的语义。

**公共读双轨灰度加持续对账。** 环境变量 `FACT_LEDGER_PUBLIC_READS`（默认 true）决定公开读走账本投影还是旧版内存过滤（implemented-at backend/internal/config/config.go:70）；`resolvePublicProjection` 在两条路径间对比比分、事件线、球队名并回调 FactProjectionAudit，投影失败时返回 integrity=conflict 的空投影而不是旧快照（implemented-at backend/internal/matchstate/fact_ledger.go:304-329）。

**用户观察隔离是接口级的。** 用户输入只调用 `observations.Record`（implemented-at backend/internal/companion/agent.go:1228-1260）；`observation.Coordinator` 接口（Record/OnFactChanged/Expire）没有任何能写 matchstate 的方法（backend/internal/observation/coordinator.go:77-94）。旧文档「用户说进球不写公共事件」不需要约定——没有那条通路。

## 与旧设计的差异

- **旧文档 §10 的命令族与微服务不存在**：`make bootstrap-ledger`、`verify-ledger-append`、`scenario-*` 演练目标和 `cmd/facts` 等独立服务从未出现；cmd/ 下只有 server/verify-datasource/eval-audit/evals/latency-test，仓库根没有 Makefile，账本不变量由包内测试和 eval 守护。真实入口是 backend/cmd/server 单体（backend/cmd/server/main.go:290-318）。concept。
- **「事实槽位」没有作为独立抽象实现**：旧文档设想显式槽位注册表；现实里槽位语义分散在事件类型校验（store.go:1486-1528）和投影 switch（fact_ledger.go:155-210）中，行为等价但没有槽位对象。
- **检查点是投影内状态而非存储**：旧文档描述可存储的受控检查点；实现里 checkpointed 只是进球投影的一个状态（fact_ledger.go:66-68, 204-209），score_correction 即隐式检查点。
- **旧文档没说但现实有的**：事件级聚合键去重的 outbox（postgres.go:235-238）；双轨公共读灰度与 FactProjectionAudit 对账（fact_ledger.go:304-316）；兼容旧接口的选边换算（conflict_resolution.go:19-49）；024 迁移的遗留数据边回填（migrations/024:18-31）。

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 追加式账本 + 场次级序号 | implemented-at | backend/migrations/022:1-12; fact_ledger.go:279-302 | code |
| 任意位置重放 + 投影版本 | implemented-at | backend/internal/matchstate/fact_ledger.go:80-97, 11 | code |
| 进球投影六态机与撤销回退 | implemented-at | backend/internal/matchstate/fact_ledger.go:62-69, 156-209 | code |
| 追加即验证（比分/证据强制） | implemented-at | backend/internal/matchstate/store.go:679-682, 1486-1528, 1445-1459 | code |
| 冲突成簇与簇合并 | implemented-at | backend/internal/matchstate/store.go:650-674, 936-998 | code |
| 冲突图三表持久化 | implemented-at | backend/migrations/023:1-20; migrations/024:1-31 | code |
| 调和=互斥约束下的可行集 | implemented-at | backend/internal/matchstate/conflict_resolution.go:51-168; fact_commands.go:28-108 | code |
| 幂等操作写与原子操作 | implemented-at | backend/cmd/server/operator_write_api.go:46-93; migrations/017:1-18 | code |
| 兼容接口强制显式选边 | implemented-at | backend/internal/matchstate/store.go:1107-1138; conflict_resolution.go:19-49 | code |
| 事务性 outbox 发布 | implemented-at | backend/internal/matchstate/postgres.go:222-239, 110-178 | code |
| fact_revisions 追加 + 裁决审计 | implemented-at | backend/internal/matchstate/postgres.go:1750-1764; migrations/027:1-10 | code |
| 双轨公共读与对账降级 | implemented-at | backend/internal/config/config.go:70; fact_ledger.go:304-329 | code |
| 用户观察隔离无写通路 | implemented-at | backend/internal/companion/agent.go:1228-1260 | code |
| make 命令族与 cmd/facts 微服务 | concept | backend/cmd/server/main.go:290-318 | doc |
