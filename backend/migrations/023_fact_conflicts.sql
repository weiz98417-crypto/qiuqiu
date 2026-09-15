CREATE TABLE IF NOT EXISTS fact_conflicts (
  id TEXT PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
  chosen_fact_id TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  resolved_at TIMESTAMPTZ,
  resolved_by TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_fact_conflicts_match_status
  ON fact_conflicts(match_id, status, detected_at DESC);

CREATE TABLE IF NOT EXISTS fact_conflict_members (
  conflict_id TEXT NOT NULL REFERENCES fact_conflicts(id) ON DELETE CASCADE,
  fact_id TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('accepted', 'candidate')),
  PRIMARY KEY (conflict_id, fact_id)
);

CREATE INDEX IF NOT EXISTS idx_fact_conflict_members_fact
  ON fact_conflict_members(fact_id, conflict_id);
