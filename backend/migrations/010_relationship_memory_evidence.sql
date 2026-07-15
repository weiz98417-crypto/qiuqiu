ALTER TABLE relationship_memories
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE INDEX IF NOT EXISTS idx_relationship_memories_active_user
  ON relationship_memories(user_id, status, expires_at, created_at DESC);
