ALTER TABLE match_events
  ADD COLUMN IF NOT EXISTS provider_event_id TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_match_events_provider_event
  ON match_events(match_id, source, provider_event_id)
  WHERE provider_event_id <> '';
