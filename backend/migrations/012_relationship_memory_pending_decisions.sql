ALTER TABLE relationship_memories
  ADD COLUMN IF NOT EXISTS pending_decision_ids TEXT[] NOT NULL DEFAULT '{}';
