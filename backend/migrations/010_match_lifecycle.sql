ALTER TABLE matches
  ADD COLUMN IF NOT EXISTS lifecycle TEXT NOT NULL DEFAULT 'draft';

CREATE INDEX IF NOT EXISTS idx_matches_lifecycle_kickoff
  ON matches(lifecycle, kickoff);
