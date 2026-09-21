-- Character settings (openspec/changes/character-settings): per-user
-- interaction-norm knobs surfaced through WS/HTTP/cue word entrances
-- (three doors, one state). Character Stance values are never stored here.
CREATE TABLE IF NOT EXISTS user_character_settings (
  user_id TEXT PRIMARY KEY,
  initiative TEXT NOT NULL DEFAULT '',
  analysis_appetite TEXT NOT NULL DEFAULT '',
  banter_level TEXT NOT NULL DEFAULT '',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
