ALTER TABLE idempotency_records
  DROP CONSTRAINT IF EXISTS idempotency_records_match_id_fkey;

ALTER TABLE idempotency_records
  ADD COLUMN IF NOT EXISTS operation TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS response JSONB,
  ADD COLUMN IF NOT EXISTS status_code INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_idempotency_records_status_expiry
  ON idempotency_records(status, expires_at);

ALTER TABLE outbox_messages
  ADD COLUMN IF NOT EXISTS event_type TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

DELETE FROM outbox_messages older
USING outbox_messages newer
WHERE older.aggregate_type = newer.aggregate_type
  AND older.aggregate_id = newer.aggregate_id
  AND older.id < newer.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_aggregate_delivery
  ON outbox_messages(aggregate_type, aggregate_id);
