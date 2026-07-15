CREATE TABLE IF NOT EXISTS relationship_states (
  user_id TEXT PRIMARY KEY,
  state JSONB NOT NULL,
  version BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS relationship_memories (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  payload JSONB NOT NULL,
  confidence DOUBLE PRECISION NOT NULL DEFAULT 1,
  source_trace_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_relationship_memories_user_kind
  ON relationship_memories(user_id, kind, created_at DESC);

CREATE TABLE IF NOT EXISTS match_companion_states (
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL,
  state JSONB NOT NULL,
  version BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, match_id)
);

CREATE TABLE IF NOT EXISTS interaction_decisions (
  id TEXT PRIMARY KEY,
  signal_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL,
  decision JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_interaction_decisions_user_match_created
  ON interaction_decisions(user_id, match_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_interaction_decisions_user_match_signal
  ON interaction_decisions(user_id, match_id, signal_id);

ALTER TABLE agent_traces
  ADD COLUMN IF NOT EXISTS relationship_decision JSONB;

ALTER TABLE conversation_turns
  ADD COLUMN IF NOT EXISTS trace_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversation_turns_trace_role
  ON conversation_turns(trace_id, role)
  WHERE trace_id <> '';
