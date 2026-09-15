CREATE TABLE IF NOT EXISTS match_clocks (
  match_id TEXT PRIMARY KEY REFERENCES matches(id) ON DELETE CASCADE,
  period TEXT NOT NULL DEFAULT 'pre_match',
  elapsed_seconds INTEGER NOT NULL DEFAULT 0,
  running BOOLEAN NOT NULL DEFAULT FALSE,
  anchor_at TIMESTAMPTZ,
  source TEXT NOT NULL DEFAULT 'system',
  version BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT match_clocks_elapsed_check CHECK (elapsed_seconds BETWEEN 0 AND 21600),
  CONSTRAINT match_clocks_period_check CHECK (period IN ('pre_match', 'first_half', 'halftime', 'second_half', 'extra_time', 'fulltime'))
);
