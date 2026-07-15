ALTER TABLE matches
  ADD COLUMN IF NOT EXISTS integrity JSONB NOT NULL DEFAULT '{"status":"ok"}'::jsonb;
