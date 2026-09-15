INSERT INTO fact_revisions (
  fact_id, revision, match_id, event_id, status, source_type, source_event_id,
  confidence, evidence, confirmed_by, revision_of, occurred_at, recorded_at, public_at
)
SELECT
  COALESCE(fact_id, id),
  GREATEST(fact_revision, 1),
  match_id,
  id,
  fact_status,
  CASE
    WHEN LOWER(source) IN ('api-sports', 'provider', 'replay') THEN 'provider'
    WHEN LOWER(source) = 'system' THEN 'system'
    ELSE 'operator'
  END,
  provider_event_id,
  confidence,
  evidence,
  confirmed_by,
  revision_of,
  created_at,
  updated_at,
  public_at
FROM match_events
ON CONFLICT (fact_id, revision) DO NOTHING;

DROP INDEX IF EXISTS idx_fact_revisions_source_event;

CREATE UNIQUE INDEX idx_fact_revisions_source_event
  ON fact_revisions(match_id, source_type, source_event_id, revision)
  WHERE source_event_id <> '';

DROP INDEX IF EXISTS idx_match_events_public_facts;

CREATE INDEX idx_match_events_public_facts
  ON match_events(match_id, public_at DESC)
  WHERE fact_status IN ('confirmed', 'reconciled') AND status = 'active';

CREATE TABLE IF NOT EXISTS match_source_cursors (
  match_id TEXT NOT NULL REFERENCES matches(id) ON DELETE CASCADE,
  source_type TEXT NOT NULL,
  source_key TEXT NOT NULL,
  cursor BIGINT NOT NULL DEFAULT 0 CHECK (cursor >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (match_id, source_type, source_key)
);

CREATE INDEX IF NOT EXISTS idx_match_source_cursors_updated
  ON match_source_cursors(updated_at DESC);
