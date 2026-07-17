CREATE TABLE IF NOT EXISTS pending_match_observations (
  id TEXT PRIMARY KEY,
  signal_id TEXT NOT NULL,
  trace_id TEXT NOT NULL DEFAULT '',
  user_id TEXT NOT NULL,
  match_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  event_type TEXT NOT NULL DEFAULT '',
  claimed_team TEXT NOT NULL DEFAULT '',
  claimed_player TEXT NOT NULL DEFAULT '',
  claimed_score_home INTEGER,
  claimed_score_away INTEGER,
  certainty TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  candidate_fact_id TEXT NOT NULL DEFAULT '',
  resolved_fact_id TEXT NOT NULL DEFAULT '',
  resolved_revision INTEGER NOT NULL DEFAULT 0,
  received_at TIMESTAMPTZ NOT NULL,
  follow_up_deadline TIMESTAMPTZ NOT NULL,
  reconcile_until TIMESTAMPTZ NOT NULL,
  resolved_at TIMESTAMPTZ,
  resolution_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT pending_match_observations_status_check CHECK (status IN (
    'pending_sync', 'corroborating', 'confirmed', 'contradicted', 'conflict', 'expired', 'superseded'
  )),
  CONSTRAINT pending_match_observations_score_check CHECK (
    (claimed_score_home IS NULL AND claimed_score_away IS NULL) OR
    (claimed_score_home IS NOT NULL AND claimed_score_away IS NOT NULL AND claimed_score_home >= 0 AND claimed_score_away >= 0)
  ),
  CONSTRAINT pending_match_observations_scope_unique UNIQUE (user_id, match_id, signal_id)
);

CREATE INDEX IF NOT EXISTS idx_pending_match_observations_reconcile
  ON pending_match_observations(match_id, status, reconcile_until);

CREATE INDEX IF NOT EXISTS idx_pending_match_observations_user
  ON pending_match_observations(user_id, match_id, received_at);
