-- 044 · RouterTrace 落库（openspec/changes/router-trace-durability）：
-- ADR-0009 的路由审计证据（意图/置信度/槽位/拒绝原因）在 Postgres 部署
-- 与内存部署同样存活。镜像 fact_claim 列的持久化模式。
ALTER TABLE agent_traces ADD COLUMN IF NOT EXISTS router JSONB;
