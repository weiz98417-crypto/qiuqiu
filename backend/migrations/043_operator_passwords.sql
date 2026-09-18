-- ADR-0010 console JWT auth (revises ADR-0008's no-password decision):
-- password login for humans, personal tokens stay the machine channel.
--
-- operators gains PBKDF2-HMAC-SHA256 credentials (100k iterations, stdlib
-- implementation validated against RFC 6070 vectors). password_set_at NULL =
-- director-issued temp password, first login forces a change. refresh_tokens
-- stores only SHA-256 hashes of 30-day refresh tokens — multi-device,
-- per-device revocation is a row deletion; rotation deletes the old row
-- (single-use). Revoking an operator cascades so no refresh row outlives
-- its identity.

ALTER TABLE operators ADD COLUMN IF NOT EXISTS password_hash TEXT;
ALTER TABLE operators ADD COLUMN IF NOT EXISTS password_set_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id BIGSERIAL PRIMARY KEY,
  operator_id BIGINT NOT NULL REFERENCES operators(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  device TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_refresh_tokens_operator
  ON refresh_tokens(operator_id);
