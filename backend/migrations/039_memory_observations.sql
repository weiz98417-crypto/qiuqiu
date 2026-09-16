CREATE TABLE IF NOT EXISTS memory_extraction_audit (
  id BIGSERIAL PRIMARY KEY,
  moment_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT '',
  importance DOUBLE PRECISION NOT NULL DEFAULT 0,
  ledger_sequence BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL,
  reason_code TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memory_extraction_audit_user_created
  ON memory_extraction_audit(user_id, created_at);

CREATE TABLE IF NOT EXISTS memory_backlog (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  attempts INT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending',
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memory_backlog_due
  ON memory_backlog(status, next_attempt_at, id);

CREATE TABLE IF NOT EXISTS memory_reflection_audit (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL DEFAULT '',
  trigger_kind TEXT NOT NULL DEFAULT '',
  insight TEXT NOT NULL DEFAULT '',
  cited_sequences JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memory_reflection_audit_user_created
  ON memory_reflection_audit(user_id, created_at);
