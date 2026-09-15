ALTER TABLE interaction_ledger
  ADD COLUMN IF NOT EXISTS trace_payload JSONB;
