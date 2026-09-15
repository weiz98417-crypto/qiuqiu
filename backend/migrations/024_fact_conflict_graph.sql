CREATE TABLE IF NOT EXISTS fact_conflict_edges (
  conflict_id TEXT NOT NULL REFERENCES fact_conflicts(id) ON DELETE CASCADE,
  left_fact_id TEXT NOT NULL,
  right_fact_id TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  detected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (conflict_id, left_fact_id, right_fact_id),
  CHECK (left_fact_id < right_fact_id),
  FOREIGN KEY (conflict_id, left_fact_id)
    REFERENCES fact_conflict_members(conflict_id, fact_id) ON DELETE CASCADE,
  FOREIGN KEY (conflict_id, right_fact_id)
    REFERENCES fact_conflict_members(conflict_id, fact_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_fact_conflict_edges_fact
  ON fact_conflict_edges(conflict_id, left_fact_id, right_fact_id);

INSERT INTO fact_conflict_edges (conflict_id, left_fact_id, right_fact_id, reason, detected_at)
SELECT
  accepted.conflict_id,
  LEAST(accepted.fact_id, candidate.fact_id),
  GREATEST(accepted.fact_id, candidate.fact_id),
  'legacy_accepted_candidate',
  conflict.detected_at
FROM fact_conflict_members accepted
JOIN fact_conflict_members candidate
  ON candidate.conflict_id = accepted.conflict_id
 AND candidate.role = 'candidate'
JOIN fact_conflicts conflict ON conflict.id = accepted.conflict_id
WHERE accepted.role = 'accepted'
ON CONFLICT (conflict_id, left_fact_id, right_fact_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS fact_conflict_resolution_facts (
  conflict_id TEXT NOT NULL REFERENCES fact_conflicts(id) ON DELETE CASCADE,
  fact_id TEXT NOT NULL,
  selected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (conflict_id, fact_id)
);

INSERT INTO fact_conflict_resolution_facts (conflict_id, fact_id, selected_at)
SELECT id, chosen_fact_id, COALESCE(resolved_at, detected_at)
FROM fact_conflicts
WHERE chosen_fact_id <> ''
ON CONFLICT (conflict_id, fact_id) DO NOTHING;
