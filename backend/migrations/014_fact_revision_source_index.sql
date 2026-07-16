DROP INDEX IF EXISTS idx_fact_revisions_source_event;

CREATE INDEX idx_fact_revisions_source_event
  ON fact_revisions(match_id, source_type, source_event_id)
  WHERE source_event_id <> '';
