-- ADR-0008 operator console identity: personal bearer tokens for operators.
--
-- Tokens are stored only as SHA-256 hex hashes (operators token_hash); the
-- plaintext is shown once at creation/bootstrap and never persisted. Role
-- carries the scope set (director = all four operator scopes, auditor =
-- TraceRead only). Revocation is a row deletion and takes effect on the next
-- request — no cached sessions. Audit rows keep the operator's name so the
-- trail survives revocation.

CREATE TABLE IF NOT EXISTS operators (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  token_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('director', 'auditor')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_operators_token_hash
  ON operators(token_hash);

-- Console writes append (operator, action, object) so every on-behalf
-- mutation (thread ops, portrait privacy-ops) stays attributable.
CREATE TABLE IF NOT EXISTS operator_audit (
  id BIGSERIAL PRIMARY KEY,
  operator_name TEXT NOT NULL,
  action TEXT NOT NULL,
  object TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_operator_audit_created
  ON operator_audit(created_at);
