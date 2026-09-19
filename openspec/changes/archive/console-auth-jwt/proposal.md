# Console Auth: JWT Login + RBAC (revises ADR-0008)

## Why

ADR-0008 chose personal bearer tokens pasted into localStorage over passwords ("internal tool, skip password complexity"). With the console becoming a 1-to-many operations management platform, that decision no longer holds: shared-token UX is friction ("token 太麻烦还一直有 BUG"), there is no login interface, and per-operator attribution starts at the wrong layer. This change adds a proper login (username + password → JWT access/refresh) with RBAC, while keeping personal tokens as the machine channel (evals depend on them).

## What Changes

- **Password auth**: `operators` gains `password_hash` (PBKDF2-HMAC-SHA256, 100k iterations, stdlib-only implementation validated against RFC 6070 test vectors) and `password_set_at`. Director creates accounts with a one-time temp password; first login forces a change; self-change requires the old password. v1: no failure lockout (failed attempts audited).
- **JWT**: login issues a 15-minute HS256 access token (minimal hand-rolled implementation: HS256 only, alg fixed — algorithm-confusion immune, ~60 lines validated against RFC 7519 vectors) + a 30-day refresh token (SHA-256 hash stored in a `refresh_tokens` table, multi-device, per-device revocation). Revoke = refresh row deletion; access dies within 15 minutes.
- **RBAC**: unchanged — roles (director/auditor) map to the four scopes; the middleware's claims source swaps from token-hash lookup to JWT claims (same Claims shape, route layer untouched). Personal tokens keep working as the machine channel.
- **Console login page**: antd login form → token gate replaced for human entry; machine-token paste moves to an "advanced" section (kept for evals/scripts).
- **Legacy compat**: eval harness env (APP_TOKEN, no operators rows) keeps legacy dual-mode verbatim; all 28 operator-control evals stay green.

## Non-goals

- No SSO/OIDC, no multi-tenant isolation (single ops team), no failure lockout in v1 (failed attempts audited only), no password recovery via email (director resets).
