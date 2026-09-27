-- 运营观测只读账号（ADR-0021）：Grafana 数据源专用，仅 SELECT 四张账本表。
-- 观测是旁路：这个账号物理上写不了任何账本。在 Postgres 16 上以超级用户
-- 执行一次（psql -f deploy/grafana/init.sql）。
--
-- 口径备注（面板 SQL 直接引用）：
--   * proactive_reminders 无 expired 状态值：missed = status 非 delivered
--     且 expire_at 已过，勿写 status='expired'。
--   * agent_traces.voice 为 JSONB：分段延迟在 voice->'latencyStages'，
--     值为相对 speech_received 的累计毫秒。
--   * interaction_ledger 无 kind 索引：按 kind 聚合走顺序扫描，30 天
--     expires_at 窗口下可接受；变慢再补部分索引（留尾）。

\if :{?qiuqiu_grafana_ro_password}
\else
\set qiuqiu_grafana_ro_password 'change-me'
\endif

DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'qiuqiu_grafana_ro') THEN
    CREATE ROLE qiuqiu_grafana_ro LOGIN PASSWORD 'change-me';
  END IF;
END
$$;

ALTER ROLE qiuqiu_grafana_ro WITH PASSWORD :'qiuqiu_grafana_ro_password';

GRANT CONNECT ON DATABASE qiuqiu_test TO qiuqiu_grafana_ro;
GRANT USAGE ON SCHEMA public TO qiuqiu_grafana_ro;
GRANT SELECT ON agent_traces TO qiuqiu_grafana_ro;
GRANT SELECT ON interaction_ledger TO qiuqiu_grafana_ro;
GRANT SELECT ON delivery_ledger TO qiuqiu_grafana_ro;
GRANT SELECT ON proactive_reminders TO qiuqiu_grafana_ro;
