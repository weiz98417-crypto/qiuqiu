ALTER TABLE interaction_ledger
  ADD COLUMN IF NOT EXISTS delivery_reason TEXT;
