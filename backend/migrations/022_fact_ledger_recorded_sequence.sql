CREATE SEQUENCE IF NOT EXISTS match_events_recorded_sequence_seq;

ALTER TABLE match_events
  ADD COLUMN IF NOT EXISTS recorded_sequence BIGINT;

WITH ranked AS (
  SELECT
    id,
    ROW_NUMBER() OVER (PARTITION BY match_id ORDER BY created_at ASC, id ASC) AS sequence
  FROM match_events
  WHERE recorded_sequence IS NULL
)
UPDATE match_events AS event
SET recorded_sequence = ranked.sequence
FROM ranked
WHERE event.id = ranked.id;

SELECT setval(
  'match_events_recorded_sequence_seq',
  GREATEST(COALESCE((SELECT MAX(recorded_sequence) FROM match_events), 0) + 1, 1),
  false
);

ALTER SEQUENCE match_events_recorded_sequence_seq
  OWNED BY match_events.recorded_sequence;

ALTER TABLE match_events
  ALTER COLUMN recorded_sequence SET DEFAULT nextval('match_events_recorded_sequence_seq'),
  ALTER COLUMN recorded_sequence SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_match_events_replay
  ON match_events(match_id, recorded_sequence);
