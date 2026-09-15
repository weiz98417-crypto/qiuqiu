ALTER TABLE agent_traces
  ADD COLUMN IF NOT EXISTS schedule JSONB,
  ADD COLUMN IF NOT EXISTS lookup_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS parent_trace_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_agent_traces_lookup
  ON agent_traces(lookup_id)
  WHERE lookup_id <> '' AND deleted_at IS NULL;
