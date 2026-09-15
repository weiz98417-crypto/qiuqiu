---
id: 24-latency-observation
title: 直播延迟与用户观察协调
source_chapter: docs/球球全套资料/4.产品设计/24-直播延迟与用户观察协调设计.md
status_summary: { implemented: 10, partial: 0, planned: 0, concept: 2 }
---

# 直播延迟与用户观察协调

本章管「用户先看到球、数据源先知道结果」时服务怎么同时做到不剧透、不忽视、不误导：核心是观察协调器（observation coordinator）把用户声明与比赛事实在时间窗口内对账。这部分是三章里实现超出旧文档的一章——协调器的状态、窗口、去重和恢复机制都比设计稿更细；但旧文档的用户剧透偏好设置和五时钟模型没有对应代码。

## 系统实际怎么工作

**用户声明只进观察，永不写事实。** 用户消息先由 `assessMatchClaim` 尝试对齐现有快照与事件；解析不出就标记 ClaimStatusUnverified，随后统一走 `recordObservation` 进入协调器（implemented-at backend/internal/companion/agent.go:826-845, 1228-1260）。协调器接口只有 Record/OnFactChanged/Expire 三个入口，没有任何一个能把观察写进 matchstate 账本——旧文档「观察不等于事实」在这里是类型级的隔离。

**七个观察状态，比旧文档的 F0-F4 更细。** 状态机是 pending_sync → corroborating → confirmed / contradicted / conflict，外加 expired 和 superseded（implemented-at backend/internal/observation/coordinator.go:15-25），数据库 CHECK 约束与之一字不差（implemented-at backend/migrations/018_pending_match_observations.sql:25-32）。观察 ID 是 `obs_` + 作用域字符串的 sha256 前 12 字节（coordinator.go:554-557），天然幂等。

**写入即去重，且有活跃上限。** Record 先按 user+match+signal 精确命中（唯一约束见 migrations/018:32），再做活跃观察语义去重——同一用户同场、同类型/球员/球队/比分的活跃观察直接返回已有记录（implemented-at backend/internal/observation/coordinator.go:119-149）。每用户每场最多 5 条活跃观察，超出时最旧的转 superseded，原因记 "pending observation limit exceeded"（同段 143-149）。

**事实变化驱动观察对账。** 事实事件只接受 provisional/confirmed/reconciled/revoked 四种状态（coordinator.go:172-181）。`applyFact` 的转移规则：provisional 候选把 pending_sync 转 corroborating；已有候选时再来一个不同候选转 conflict（"multiple matching candidate facts"）；confirmed/reconciled 命中即 confirmed 并绑定 factId+revision；revoked 命中即 contradicted，且跟进窗口至少延长 45 秒给更正留时间（implemented-at backend/internal/observation/coordinator.go:493-552）。同一事实同一版本的重复事件经 repeatedResolution 幂等重放交付（410-425）。

**反向矛盾有专门通道。** 用户说进球了、随后确认的却是 shot/save/miss/goal_cancelled——`matchesGoalContradiction` 直接把观察转 contradicted，reason 为 "matched confirmed non-goal fact"（implemented-at backend/internal/observation/coordinator.go:467-481）。这是旧文档「用户领先来源」场景最尖锐的分支：承认用户看见了什么，同时不迁就他对结果的解读。

**窗口是按事件类型的真值表，且随数据源健康缩放。** var_result/goal_cancelled 给 45 秒跟进 + 2 分钟调和；shot/save/黄牌/换人给 10 秒 + 45 秒；其余默认 15 秒 + 1 分钟（implemented-at backend/internal/observation/coordinator.go:326-348）。数据源离线或降级时，调和窗口被放大到 90 秒至 2 分钟（implemented-at backend/internal/datasource/manager.go:160-186）——旧文档「来源越不可靠越保守」在这里是具体的秒数。

**过期与恢复都是服务端闭环。** 服务启动后开一个 5 秒间隔的 Expire 循环，把超过 ReconcileUntil 的 pending_sync/corroborating/conflict 转 expired（implemented-at backend/cmd/server/main.go:1063-1076, coordinator.go:299-324）。确认/矛盾产出带 deliveryKey（observationID:revision:status）的 Resolution，持久化到 observation_resolution_outbox（implemented-at backend/migrations/020_observation_resolution_outbox.sql:1-14）；客户端重连时经 RecoverObservationFollowUps 按窗口恢复投递（implemented-at backend/internal/companion/agent.go:431-452）。

**可靠文案和表演计划是确定性的。** 确认文案按事件类型生成（射门/扑救/进球者各有固定句式），撤销统一是「结果出来了，这球没算」（implemented-at backend/internal/observation/coordinator.go:441-465）；confirmed 绑 excited/cheer 表演、contradicted 绑 deflated/slump（implemented-at backend/internal/companion/agent.go:491-500）。

**Postgres 路径带隐私事务。** PostgresCoordinator 的 Record 在同一事务内先 `privacy.LockUserTx` + `privacy.CheckDeletionTx` 再读写观察行（implemented-at backend/internal/observation/postgres.go:46-67）——用户删除资料后不会有新观察写入。协调器本身由配置开关启用（backend/cmd/server/main.go:306-318），事实变化经事件观察者回调驱动（361-368）。

## 与旧设计的差异

- **实现比旧文档细**：旧文档只有 F0-F4 五层；代码是七状态机 + 按事件类型的窗口真值表 + 5 条活跃上限 + deliveryKey 恢复（backend/internal/observation/coordinator.go:15-25, 326-348, 143-149）。
- **五时钟是概念不是代码**：T0/Tu/Ts/Td/Tr 没有对应类型；实际用 ReceivedAt/FollowUpDeadline/ReconcileUntil 三个时间戳近似（backend/internal/observation/coordinator.go:42-63）。concept。
- **用户剧透偏好设置不存在**：严格防剧透/有限提示/事实优先三档设置没有实现；现实是单向保守——用户永远只拿到已确认事实，主动推送由操作员 AutomationPolicy 决定，与用户偏好无关（backend/internal/conversation/proactive_gate.go:17-19）。concept。
- **旧文档没说但现实有的**：观察写入带用户隐私事务锁（backend/internal/observation/postgres.go:46-67）；观察回应可被语音回合抑制（backend/internal/observation/coordinator.go:237-266）；跨用户隔离靠「观察记录键含 userID」这一结构事实保证，没有群组同步通路。
- **来源分级表（8.1）未按表实现**：来源对协调器的影响只有一条路径——数据源健康放大调和窗口（backend/internal/datasource/manager.go:160-186），没有「权威来源可直接确认」的快车道。

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 用户声明只进观察不写事实 | implemented-at | backend/internal/companion/agent.go:826-845, 1228-1260 | code |
| 七个观察状态与 DB 约束一致 | implemented-at | backend/internal/observation/coordinator.go:15-25; migrations/018:25-32 | code |
| 幂等去重 + 5 条活跃上限 | implemented-at | backend/internal/observation/coordinator.go:119-149; migrations/018:32 | code |
| 事实事件驱动状态机转移 | implemented-at | backend/internal/observation/coordinator.go:493-552 | code |
| 反向矛盾（进球被判非进球） | implemented-at | backend/internal/observation/coordinator.go:467-481 | code |
| 按事件类型的窗口 + 数据源放大 | implemented-at | backend/internal/observation/coordinator.go:326-348; datasource/manager.go:160-186 | code |
| 5 秒过期循环 + 交付恢复 | implemented-at | backend/cmd/server/main.go:1063-1076; agent.go:431-452; migrations/020:1-14 | code |
| 确定性可靠文案与表演计划 | implemented-at | backend/internal/observation/coordinator.go:441-465; agent.go:491-500 | code |
| 回应抑制与撤销收敛 | implemented-at | backend/internal/observation/coordinator.go:237-266; cmd/server/main.go:541-549 | code |
| Postgres 协调器隐私事务 | implemented-at | backend/internal/observation/postgres.go:46-67 | code |
| 五时钟模型 | concept | backend/internal/observation/coordinator.go:42-63 | doc |
| 用户剧透偏好设置 | concept | backend/internal/conversation/proactive_gate.go:17-19 | doc |
