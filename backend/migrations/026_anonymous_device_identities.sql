CREATE TABLE IF NOT EXISTS anonymous_device_identities (
  device_id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO anonymous_device_identities (device_id, user_id, expires_at, created_at, updated_at)
SELECT DISTINCT ON (device_id)
  device_id,
  user_id,
  expires_at,
  created_at,
  created_at
FROM user_sessions
WHERE revoked_at IS NULL AND expires_at > now()
ORDER BY device_id, created_at DESC
ON CONFLICT (device_id) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_anonymous_device_identities_expiry
  ON anonymous_device_identities(expires_at);
