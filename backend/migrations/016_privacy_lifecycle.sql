ALTER TABLE privacy_tombstones
  ADD COLUMN IF NOT EXISTS job_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

CREATE UNIQUE INDEX IF NOT EXISTS idx_privacy_tombstones_job
  ON privacy_tombstones(job_id)
  WHERE job_id <> '';

CREATE INDEX IF NOT EXISTS idx_privacy_tombstones_pending
  ON privacy_tombstones(status, requested_at)
  WHERE status = 'pending';
