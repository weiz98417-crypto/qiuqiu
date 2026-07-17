CREATE TABLE IF NOT EXISTS observation_resolution_outbox (
  delivery_key TEXT PRIMARY KEY,
  observation_id TEXT NOT NULL REFERENCES pending_match_observations(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL,
  resolution_status TEXT NOT NULL,
  fact_id TEXT NOT NULL DEFAULT '',
  fact_revision INTEGER NOT NULL DEFAULT 0,
  reliable_text TEXT NOT NULL,
  follow_up_deadline TIMESTAMPTZ NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  delivered_at TIMESTAMPTZ,
  CONSTRAINT observation_resolution_outbox_status_check CHECK (status IN ('pending', 'delivered', 'suppressed'))
);

CREATE INDEX IF NOT EXISTS idx_observation_resolution_outbox_pending
  ON observation_resolution_outbox(user_id, match_id, follow_up_deadline)
  WHERE status = 'pending';
