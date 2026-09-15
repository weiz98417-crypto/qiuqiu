---
id: 56-postgres-migrations
title: PostgreSQL 持久化、迁移与数据生命周期（事实源）
source_chapter: docs/球球全套资料/5.AI Coding工程实践/56-PostgreSQL持久化、迁移与数据生命周期.md
status_summary:
  implemented: 9
  partial: 1
  planned: 0
  concept: 3
---

# PostgreSQL 持久化、迁移与数据生命周期（事实源）

这套系统的持久化历史不写在设计文档里，写在 `backend/migrations` 的 40 个 SQL 文件里：从陪看记忆（001）到互动/投递账本（031-038），每个迁移都是一次真实的能力合入。本篇按真实迁移序列与 `backend/internal/matchstate/postgres.go`（2151 行）讲解模式如何演化、outbox 如何投递、删除如何生效。

## 系统实际怎么工作

**迁移即演化史。** 40 个顺序 SQL（编号 001-038，其中 010/011/012 各有两文件、025 缺号）分期合入：001-005 陪看记忆与赛事自动化（001 启用 pgvector，backend/migrations/001_companion_match_memory.sql:1-2）；006-012 关系导播与记忆证据（006 建 relationship_states/relationship_memories，006_relationship_director.sql:1-13）；**013_trusted_interaction_foundation** 一次建立 user_sessions、fact_revisions、idempotency_records、outbox_messages、privacy_tombstones 五张核心表（013:1,51,76,90,107）；014-015 事实修订索引与兼容；016 隐私生命周期；017 导播幂等与 outbox 唯一投递（017:4-27）；018-020 直播观察协调（018 pending_match_observations、020 observation_resolution_outbox）；021-028 事实账本单调序（022）与冲突调和（023/024/027/028）；026 匿名设备身份；031-038 互动账本与投递账本（031_interaction_ledger.sql:1-24、032_delivery_ledger.sql:1-11）。

**迁移执行器。** 没有 migrate CLI：服务启动时 `OpenPostgresStore(ctx, dbURL, "migrations")`（backend/cmd/server/main.go:216，镜像内 COPY migrations，backend/Dockerfile:21），执行器按文件名排序、开单事务、取 `pg_advisory_xact_lock` 串行化、建 `schema_migrations` 表逐个应用并记录（backend/internal/matchstate/postgres.go:1639-1685）。任一文件失败即整体回滚。

**事务性 outbox。** 事实写入与出站消息同一事务落库；独立 worker `RunOutbox` 每 250ms 轮询（postgres.go:110-126），`FOR UPDATE SKIP LOCKED` 领取一条 pending 消息（postgres.go:145-157），写 30 秒租约后提交再发布（postgres.go:166-173），失败按 `1<<min(attempts-1,6)` 秒指数退避、10 次后置 failed（postgres.go:189-199）。启动入口在 main.go:369-370。

**操作原子性。** 导播写入经 `operatorwrite` 包携带事务上下文，`beginMutation` 优先复用外部事务而非新开（postgres.go:89-108），同一操作的账本写入与出站意图因此原子。

**隐私生命周期。** privacy 包提供 Status/Export/RequestDeletion/ProcessDeletion/CleanupExpired（backend/internal/privacy/service.go:75-111）；保留期 `PRIVACY_RETENTION_DAYS` 默认 30 天（backend/internal/config/config.go:68，docker-compose.yml:25）；016 给墓碑表补 job_id 与 pending 部分索引（016:1-13）；墓碑在 auth 与 privacy 的写入路径被查询（backend/internal/privacy/postgres.go:199-213,247,275-292）。

## 与旧设计的差异

| 旧设计主张 | 状态 | 现实 |
| --- | --- | --- |
| 「38 个迁移」 | 漂移 | 实测 40 个文件（010/011/012 重号、缺 025），backend/migrations 目录即证据 |
| 六个持久化域 + principals/session_controls/access_grants/device_contexts/source_observations/fact_snapshots/preferences/memory_items/consent_records/audit_entries 等表 | concept | 这些表从未创建（旧章:154-282）。真实对应物：user_sessions(013:1)、relationship_states(006:1)、pending_match_observations(018:1)、interaction_ledger(031:1)、anonymous_device_identities(026:1)；"偏好/共同记忆"由 relationship_memories 承载而非独立 preferences 域 |
| 四阶段迁移流程（扩展/双读写/回填/收紧）与迁移元数据表 | concept | 执行器只有"排序+单事务+advisory lock+历史表"（postgres.go:1639-1685）；回填直接写成幂等 SQL 迁移（如 022:5-10、028），无双写窗口机制 |
| 事实发布七步事务流程图 | partial | 原子性以 operatorwrite 事务复用实现（postgres.go:89-108）+ outbox 唯一索引（017:19-27），无旧图中的"角色/场次范围校验链" |
| 删除先建屏障、备份恢复后重放屏障、恢复演练五场景 | partial | 墓碑写入与查询真实存在（privacy/postgres.go:199-292，companion/privacy_integration_test.go），但备份/恢复/演练整体缺失 |
| 备份目标表、恢复流程、RTO/RPO 假设 | concept | docker-compose 仅挂 postgres_data 卷（docker-compose.yml:38-41），scripts/ 无任何备份恢复脚本 |
| R0-R5 六级保留分层 | concept | 只有互动账本 30 天 expires_at（031:24）与 PRIVACY_RETENTION_DAYS 30 天（config.go:68）两处期限 |
| 旧章「fact_conflicts 未解时禁止结论性投影」状态机 | partial | 023 的 status CHECK 仅 open/resolved 两态（023:4），决议走 024 冲突边图 + 027/028 审计回填，比旧描述更窄但可执行 |

## 主张-锚点表

| # | 主张 | status | 锚点 |
| --- | --- | --- | --- |
| 1 | 40 个顺序 SQL 迁移，001-038 重号/缺号 | implemented-at | backend/migrations; 010_match_lifecycle.sql; 010_relationship_memory_evidence.sql |
| 2 | 迁移序列即演化史（记忆→自动化→可信地基→事实账本→投递账本） | implemented-at | 001:1-2; 006:1-13; 013:1,51,76,90,107; 018:1-13; 031:1-24; 032:1-11 |
| 3 | 013 一迁移建五张核心表 | implemented-at | backend/migrations/013_trusted_interaction_foundation.sql:1,51,76,90,107 |
| 4 | 022-024/027-028 账本单调序与冲突调和 | implemented-at | 022:1-10; 023:1-19; 024:1-16; 027:1-14; backend/internal/matchstate/conflict_resolution.go |
| 5 | 017 幂等升级 + outbox 聚合唯一索引 | implemented-at | backend/migrations/017_operator_idempotency_outbox.sql:4-27 |
| 6 | Go 端迁移执行器（排序/单事务/advisory lock/schema_migrations） | implemented-at | postgres.go:1639-1685; backend/cmd/server/main.go:216; backend/Dockerfile:21 |
| 7 | 事务性 outbox：250ms 轮询/SKIP LOCKED/30s 租约/指数退避/10 次失败 | implemented-at | postgres.go:110-126,145-157,166-173,189-199; backend/cmd/server/main.go:369-370 |
| 8 | 同操作共享事务（operatorwrite 复用） | implemented-at | postgres.go:89-108; backend/internal/operatorwrite |
| 9 | 隐私五操作 + 30 天保留 + 016 墓碑索引 | implemented-at | privacy/service.go:75-111; config/config.go:68; docker-compose.yml:25; 016:1-13 |
| 10 | 删除屏障真实、恢复重放缺失 | partial | privacy/postgres.go:199-213,247,275-292; backend/internal/auth/postgres.go; backend/internal/companion/privacy_integration_test.go |
| 11 | 旧章想象表从未创建 | concept | backend/migrations; 旧章:154-282 |
| 12 | 备份/恢复/灾难演练不存在 | concept | docker-compose.yml:38-41; scripts |
| 13 | R0-R5 保留分层未实现 | concept | 031_interaction_ledger.sql:24; config/config.go:68 |
