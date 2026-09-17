# Design: Console Auth JWT

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | Password hash: PBKDF2-HMAC-SHA256, 100k iterations, 16-byte salt, implemented on stdlib (crypto/hmac + crypto/sha256), validated against RFC 6070 vectors — zero-dependency discipline holds. |
| 2 | JWT: minimal hand-rolled HS256 (fixed alg, RFC 7519 shape, base64url), 15-minute access tokens; rejects any other alg by construction. |
| 3 | Refresh: 30-day refresh tokens, SHA-256 hashed in a `refresh_tokens` table (operator_id, device label, created_at, revoked) — multi-device, per-device revocation; revoke-all on operator disable. |
| 4 | Dual channel: personal tokens = machine channel (evals/scripts, unchanged); JWT = human channel. Both produce the same `auth.Claims` → per-route scope enforcement untouched. |
| 5 | Lifecycle: director creates account (temp password shown once) → first login forces change; self-change requires old password; failed logins audited, no lockout in v1. |

## Schema (migration 043)

```sql
ALTER TABLE operators ADD COLUMN password_hash TEXT;
ALTER TABLE operators ADD COLUMN password_set_at TIMESTAMPTZ;
refresh_tokens(id, operator_id → operators(id), token_hash, device, created_at, expires_at, revoked_at)
```

## Routes

- `POST /api/console/auth/login` `{username, password}` → `{accessToken, refreshToken, operator{name, role, scopes}}`; 401 on bad credentials; failure appended to audit.
- `POST /api/console/auth/refresh` `{refreshToken}` → new pair; revoked/expired → 401.
- `POST /api/console/auth/logout` `{refreshToken}` → revoke that device row.
- `PATCH /api/console/me/password` `{oldPassword, newPassword}` → self-change (old required; new min length 10).
- JWT claims: `{sub: operator name, role, scopes: [...], exp, iat}` — the authz layer resolves claims from a valid JWT **or** a valid personal token (machine channel), identical downstream.

## PBKDF2 note

RFC 2898/8018 PBKDF2 with HMAC-SHA256 is ~20 lines on stdlib (`hmac.New(sha256.New)` iterated); validation vectors from RFC 6070 test cases guarantee correctness. 100k iterations ≈ 60–100ms on a dev machine — login-only cost.

## JWT note

Hand-rolled HS256 covers exactly: header `{"alg":"HS256","typ":"JWT"}`, payload `{sub, role, scopes, exp, iat}`, signature = HMAC-SHA256(header.payload, server secret). `SECURITY: no alg field negotiation` — anything ≠ HS256 is rejected before signature check (the alg-confusion class is structurally absent). Server secret from env `QIUQIU_JWT_SECRET` (required when any password account exists).

## Tests

- pbkdf2_test: RFC 6070 vectors; length/salt uniqueness.
- jwt_test: sign/verify round trip; expired; tampered payload; wrong-alg header rejected; missing secret refused.
- login_test: happy path, bad password (401 + audit row), first-login force-change flag, refresh rotation (old refresh token single-use), revoke-all.
- compat: personal-token requests still authorized; mixed-mode (one operator with password, another token-only).
