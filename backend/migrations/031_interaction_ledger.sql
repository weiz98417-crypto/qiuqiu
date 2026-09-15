CREATE TABLE IF NOT EXISTS interaction_ledger (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  signal_id TEXT,
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL,
  trace_id TEXT,
  decision_id TEXT,
  fact_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  fact_revision TEXT,
  delivery_key TEXT,
  delivery_state TEXT,
  media_type TEXT,
  playback_state TEXT,
  stale BOOLEAN NOT NULL DEFAULT FALSE,
  source TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '30 days')
);

CREATE INDEX IF NOT EXISTS idx_interaction_ledger_user_match_created
  ON interaction_ledger(user_id, match_id, created_at);
