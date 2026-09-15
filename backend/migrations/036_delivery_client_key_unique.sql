ALTER TABLE delivery_ledger
  ADD COLUMN IF NOT EXISTS client_key_enforced BOOLEAN;

UPDATE delivery_ledger
SET client_key_enforced = FALSE
WHERE client_key_enforced IS NULL;

ALTER TABLE delivery_ledger
  ALTER COLUMN client_key_enforced SET DEFAULT TRUE;

ALTER TABLE delivery_ledger
  ALTER COLUMN client_key_enforced SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_ledger_client_key_unique
  ON delivery_ledger(user_id, match_id, client_delivery_key)
	WHERE client_key_enforced AND client_delivery_key IS NOT NULL AND client_delivery_key <> '';
