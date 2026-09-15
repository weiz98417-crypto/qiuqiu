CREATE TABLE IF NOT EXISTS user_sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  device_id TEXT NOT NULL,
  token_hash BYTEA NOT NULL,
  scopes TEXT[] NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_sessions_token_hash
  ON user_sessions(token_hash);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_active
  ON user_sessions(user_id, expires_at DESC)
  WHERE revoked_at IS NULL;

ALTER TABLE match_events
  ADD COLUMN IF NOT EXISTS fact_id TEXT,
  ADD COLUMN IF NOT EXISTS fact_revision INT NOT NULL DEFAULT 1,
  ADD COLUMN IF NOT EXISTS fact_status TEXT NOT NULL DEFAULT 'provisional',
  ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN IF NOT EXISTS confirmed_by TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS public_at TIMESTAMPTZ;

UPDATE match_events
SET fact_id = id
WHERE fact_id IS NULL OR fact_id = '';

UPDATE match_events
SET fact_status = CASE WHEN confirmed THEN 'confirmed' ELSE 'provisional' END
WHERE fact_status = 'provisional';

UPDATE match_events
SET public_at = created_at
WHERE fact_status = 'confirmed' AND public_at IS NULL;

ALTER TABLE match_events
  DROP CONSTRAINT IF EXISTS match_events_fact_status_check;

ALTER TABLE match_events
  ADD CONSTRAINT match_events_fact_status_check
  CHECK (fact_status IN ('provisional', 'confirmed', 'revoked', 'conflict', 'reconciled'));

CREATE INDEX IF NOT EXISTS idx_match_events_public_facts
  ON match_events(match_id, public_at DESC)
  WHERE fact_status = 'confirmed' AND status = 'active';

CREATE TABLE IF NOT EXISTS fact_revisions (
  fact_id TEXT NOT NULL,
  revision INT NOT NULL,
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  event_id TEXT REFERENCES match_events(id) ON DELETE SET NULL,
  status TEXT NOT NULL CHECK (status IN ('provisional', 'confirmed', 'revoked', 'conflict', 'reconciled')),
  source_type TEXT NOT NULL CHECK (source_type IN ('operator', 'provider', 'system')),
  source_event_id TEXT NOT NULL DEFAULT '',
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
  evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
  confirmed_by TEXT NOT NULL DEFAULT '',
  revision_of TEXT,
  occurred_at TIMESTAMPTZ,
  recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  public_at TIMESTAMPTZ,
  PRIMARY KEY (fact_id, revision)
);

CREATE INDEX IF NOT EXISTS idx_fact_revisions_match_status
  ON fact_revisions(match_id, status, recorded_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_fact_revisions_source_event
  ON fact_revisions(match_id, source_type, source_event_id)
  WHERE source_event_id <> '';

CREATE TABLE IF NOT EXISTS idempotency_records (
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  idempotency_key TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  result_event_id TEXT,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'failed')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (match_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_idempotency_records_expiry
  ON idempotency_records(expires_at);

CREATE TABLE IF NOT EXISTS outbox_messages (
  id BIGSERIAL PRIMARY KEY,
  aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'published', 'failed')),
  attempts INT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ,
  last_error TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_outbox_messages_pending
  ON outbox_messages(next_attempt_at, created_at)
  WHERE status = 'pending';

CREATE TABLE IF NOT EXISTS privacy_tombstones (
  user_id TEXT PRIMARY KEY,
  scope TEXT NOT NULL DEFAULT 'all',
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'failed')),
  reason TEXT NOT NULL DEFAULT 'user_request',
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);

ALTER TABLE conversation_turns
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE agent_traces
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE relationship_states
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE relationship_memories
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE match_companion_states
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

ALTER TABLE interaction_decisions
  ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_conversation_turns_expiry
  ON conversation_turns(expires_at)
  WHERE deleted_at IS NULL AND expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_agent_traces_user_expiry
  ON agent_traces(user_id, expires_at)
  WHERE deleted_at IS NULL AND expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_relationship_memories_user_expiry
  ON relationship_memories(user_id, expires_at)
  WHERE deleted_at IS NULL AND expires_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_interaction_decisions_user_expiry
  ON interaction_decisions(user_id, expires_at)
  WHERE deleted_at IS NULL AND expires_at IS NOT NULL;
