CREATE TABLE IF NOT EXISTS delivery_ledger (
  delivery_key TEXT PRIMARY KEY,
  client_delivery_key TEXT,
  trace_id TEXT,
  match_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  state TEXT NOT NULL,
  critical BOOLEAN NOT NULL DEFAULT FALSE,
  expires_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_delivery_ledger_user_match_pending
  ON delivery_ledger(user_id, match_id, state, updated_at);
