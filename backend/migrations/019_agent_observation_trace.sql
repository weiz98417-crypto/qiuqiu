ALTER TABLE agent_traces
  ADD COLUMN IF NOT EXISTS pending_observation JSONB,
  ADD COLUMN IF NOT EXISTS observation_resolution JSONB;
