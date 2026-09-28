-- 最小 PG 集成测试 schema（companion 包 AttachVoice 集成测试专用）：
-- 只建 WriteTrace/GetTrace/AttachVoice 触及的表与列（列形 = 全量迁移链
-- 演化后的最终形），不含 vector 依赖——本机 portable PG 装不了 pgvector
-- （MinGW 编不出扩展），全量 migrations 在开发机跑不了；部署轮用真镜像
-- 跑全量。跑法：DATABASE_URL=... go test ./internal/companion/ -run Postgres。

CREATE TABLE IF NOT EXISTS matches (
  id TEXT PRIMARY KEY,
  home_team TEXT NOT NULL DEFAULT '',
  away_team TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_traces (
  id TEXT PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL DEFAULT '',
  input TEXT NOT NULL,
  intent TEXT NOT NULL,
  tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb,
  retrieved_event_ids TEXT[] NOT NULL DEFAULT '{}',
  output TEXT NOT NULL,
  reason TEXT NOT NULL,
  latency_ms INT NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  voice JSONB,
  fact_claim JSONB,
  pending_observation JSONB,
  observation_resolution JSONB,
  relationship_decision JSONB,
  schedule JSONB,
  lookup_id TEXT NOT NULL DEFAULT '',
  parent_trace_id TEXT NOT NULL DEFAULT '',
  router JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_agent_traces_match_created
  ON agent_traces(match_id, created_at DESC);

CREATE TABLE IF NOT EXISTS conversation_turns (
  id BIGSERIAL PRIMARY KEY,
  trace_id TEXT NOT NULL DEFAULT '',
  match_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  role TEXT NOT NULL,
  text TEXT NOT NULL,
  event_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_turns_trace_role
  ON conversation_turns(trace_id, role) WHERE trace_id <> '';

CREATE TABLE IF NOT EXISTS privacy_tombstones (
  user_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  job_id TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
