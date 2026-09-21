-- Semantic memory vectors (openspec/changes/semantic-memory): Moment
-- embeddings for the pgvector recall path. The contains scoring stays as the
-- dual-path partner (ADR-0006 second-adapter door); this table is the only
-- new state and is fully droppable (recall degrades to contains).
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS embedding_moments (
  id BIGSERIAL PRIMARY KEY,
  user_id TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  importance REAL NOT NULL DEFAULT 0,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  embedding vector(1024) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_embedding_moments_user
  ON embedding_moments(user_id, occurred_at DESC);
