ALTER TABLE match_players
  ADD COLUMN IF NOT EXISTS lineup TEXT NOT NULL DEFAULT 'starter'
  CHECK (lineup IN ('starter', 'bench'));
