---
id: 23-match-facts-public-score
title: 比赛事实与公开比分
source_chapter: docs/球球全套资料/4.产品设计/23-比赛事实与公开比分设计.md
status_summary: { implemented: 10, partial: 1, planned: 0, concept: 1 }
---

# 比赛事实与公开比分

本章管「一个进球如何变成用户可以相信的公开比分」：事实状态枚举、公开事实投影、完整性状态与更正收敛。这条链在 Go 后端已基本落地——五值事实状态、投影快照、撤销收敛全部有真实代码；旧文档的来源分层和四类信息卡则大部分停留在设计层。

## 系统实际怎么工作

**事实状态是五值枚举，不是中文状态词典。** 每个比赛事件带 `FactStatus`，取值 provisional/confirmed/revoked/conflict/reconciled（implemented-at backend/internal/matchstate/store.go:29-35）。旧文档里的「待确认」对应 provisional，「有争议」对应 conflict，「已更正」不是独立事实状态而是事件级的 `Status: "corrected"` 标记。入库时状态自动推导：来源为空、operator/manual/system 或已确认标记的事件默认 confirmed，其余外部来源默认 provisional（implemented-at backend/internal/matchstate/store.go:1341-1361）。

**公开事实有严格准入。** 只有 `Status == "active"`、`Visibility == "public"` 且事实状态为 confirmed/reconciled 的事件才算公开事实（implemented-at backend/internal/matchstate/store.go:1363-1368）；公共快照的投影循环只消费 `IsPublicFact` 通过的事件（implemented-at backend/internal/matchstate/fact_ledger.go:150-156）。所以一个 provisional 候选无论怎么写入，都不会改变用户看到的比分。

**比分是投影，不是字段。** 比分由账本重放计算：每个公开进球把对应队伍 +1；`goal_cancelled` 通过 `RevisionOf` 找到原进球并把比分 -1；`score_correction` 直接设定新比分并把此前活跃进球检查点化（implemented-at backend/internal/matchstate/fact_ledger.go:156-213）。追加时强校验：进球事件的比分必须恰好等于「当前投影 +1」，goal_cancelled 必须恰好 -1，其余事件类型不得改比分（implemented-at backend/internal/matchstate/store.go:1486-1528）。这比旧文档「只有进球类有效事件可改变比分」的不变量更硬——不是约定，是会拒绝写入的代码。

**快照自带完整性元数据。** `Snapshot` 携带 integrity(ok/conflict)、`ProjectionVersion`、`ProjectedSequence`、LastPublicDescription 等字段（implemented-at backend/internal/matchstate/store.go:201-220）。当投影规则本身失败（例如历史数据自相矛盾）时，系统不回退旧值，而是产出 integrity=conflict、reason="public projection unavailable" 的空投影（implemented-at backend/internal/matchstate/fact_ledger.go:318-329）——宁可给状态不给数字。

**状态转移受状态机约束。** 撤销可从 provisional/confirmed/conflict/reconciled 任意有效态进入；reconciled 只能从 conflict 进入；其余转移一律 `fact cannot transition from %s to %s` 报错（implemented-at backend/internal/matchstate/store.go:1472-1484）。

**冲突让比分冻结而不是选边。** 同事件类型、45 秒时钟窗内、来自不同源且队伍/球员互斥的候选触发 ErrConflict：新事件被标为 conflict，快照 integrity 转 conflict（implemented-at backend/internal/matchstate/store.go:650-674，判定逻辑 1567-1585）。冲突簇的调和见第 47 章。

**每次状态变化都留修订链。** 每次事实状态变化都向 fact_revisions 追加一行，按 (fact_id, revision) 去重（implemented-at backend/internal/matchstate/postgres.go:1750-1764）；操作员可通过 `GET /api/matches/{id}/facts/{factId}/revisions` 查询整条修订史（implemented-at backend/cmd/server/main.go:1826-1833）。

**撤销是显式收敛，不是覆盖。** 非公开事实变化时服务端推 `match_snapshot`；若事实状态为 revoked，再追加一条 `match_fact_retracted`（implemented-at backend/cmd/server/main.go:541-549），客户端收到后把该事件从已展示反应中移除（implemented-at client/lib/screens/match_screen.dart:343）。

**主动发布由操作员策略门控。** 自动发布资格由 `AutomationPolicy` 决定：active/paused 模式、事件类型白名单（goal/var_result/goal_cancelled 等 20 种）、冷却 0-300 秒（implemented-at backend/internal/matchstate/store.go:2001-2051）；消费方 ProactiveGate 只放行策略内事件（implemented-at backend/internal/conversation/proactive_gate.go:17-19）。

## 与旧设计的差异

- **枚举名漂移**：旧文档状态词典用中文（待确认/已确认/有争议/已更正/已撤销/不可判断/不可用）；代码是五值英文枚举 + 事件级 corrected 标记，「不可判断」「不可用」没有对应枚举（backend/internal/matchstate/store.go:29-35）。
- **来源分层 S0-S4 未实现**：代码没有来源等级门槛，来源只是字符串身份记入修订历史（backend/internal/matchstate/store.go:1179-1187）；冲突判定靠事件类型 + 45 秒时钟窗（backend/internal/matchstate/store.go:1567-1585），不按来源层级提高确认门槛。partial。
- **四类信息卡与剧透偏好是纸面设计**：客户端只有 factStatus/factRevision 交付键（client/lib/services/match_session_controller.dart:1345-1351）和 match_fact_retracted 处理（client/lib/screens/match_screen.dart:343）；「待确认提示卡」「进度保护入口」和用户剧透设置没有对应组件。concept。
- **旧文档没说但现实有的**：快照带投影版本与投影序号（backend/internal/matchstate/store.go:201-220）；投影失败时给 integrity=conflict 空投影而非旧值（backend/internal/matchstate/fact_ledger.go:318-329）；主动发布是操作员 AutomationPolicy 白名单而非用户侧资格链（backend/internal/conversation/proactive_gate.go:17-19）。

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 五值事实状态枚举 | implemented-at | backend/internal/matchstate/store.go:29-35 | code |
| 来源自动推导 provisional/confirmed | implemented-at | backend/internal/matchstate/store.go:1341-1361 | code |
| 公开事实准入与投影过滤 | implemented-at | backend/internal/matchstate/store.go:1363-1377; fact_ledger.go:150-156 | code |
| 比分是投影（进球/取消/修正） | implemented-at | backend/internal/matchstate/fact_ledger.go:156-213 | code |
| 快照携带 integrity + 投影失败降级 | implemented-at | backend/internal/matchstate/store.go:201-220; fact_ledger.go:318-329 | code |
| 状态机转移约束 | implemented-at | backend/internal/matchstate/store.go:1472-1484 | code |
| 跨源冲突冻结比分 | implemented-at | backend/internal/matchstate/store.go:650-674, 1567-1585 | code |
| 修订历史追加与查询 | implemented-at | backend/internal/matchstate/postgres.go:1750-1764; cmd/server/main.go:1826-1833 | code |
| 撤销推送 match_fact_retracted | implemented-at | backend/cmd/server/main.go:541-549; client/lib/screens/match_screen.dart:343 | code |
| 自动发布由 AutomationPolicy 门控 | implemented-at | backend/internal/matchstate/store.go:2001-2051; proactive_gate.go:17-19 | code |
| 来源分层 S0-S4 等级门槛 | partial | backend/internal/matchstate/store.go:1567-1585, 1179-1187 | code |
| 四类信息卡与剧透偏好设置 | concept | client/lib/services/match_session_controller.dart:1345-1351 | doc |
