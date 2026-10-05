-- Reflection citation sequences (agent-internals 3.5): the per-user ledger
-- sequences pending their next reflection beat. Previously in-process only —
-- a restart silently dropped them and the reflection audit chain broke
-- (agent-internals A5). Physical rows, no tombstones: takeCitations deletes
-- what it consumed.
CREATE TABLE IF NOT EXISTS memory_citations (
  user_id TEXT NOT NULL,
  ledger_sequence BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, ledger_sequence)
);

CREATE INDEX IF NOT EXISTS idx_memory_citations_user
  ON memory_citations(user_id, created_at);
