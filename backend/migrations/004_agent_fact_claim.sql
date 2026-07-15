ALTER TABLE agent_traces
  ADD COLUMN IF NOT EXISTS fact_claim JSONB;
