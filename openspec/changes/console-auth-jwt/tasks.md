# Tasks: Console Auth JWT

## 1. Backend

- [ ] 1.1 Migration 043: `operators.password_hash` + `password_set_at`; `refresh_tokens` table.
- [ ] 1.2 `internal/consoleauth` (or operatorauth extension): pbkdf2.go (stdlib PBKDF2-HMAC-SHA256 + RFC 6070 vector tests), jwt.go (minimal HS256 sign/verify, fixed alg, expiry + claims), tests.
- [ ] 1.3 Routes: POST auth/login (verify PBKDF2, issue access+refresh, failure audit), POST auth/refresh (rotation: old refresh single-use), POST auth/logout (revoke device row), PATCH me/password (old-password required).
- [ ] 1.4 Middleware: claims resolution accepts JWT (priority) or personal token (machine channel) — same Claims shape; scope enforcement unchanged.
- [ ] 1.5 Env `QIUQIU_JWT_SECRET` required when any password account exists; document in deploy README.
- [ ] 1.6 Tests: login/refresh/rotate/revoke; expired access accepted via refresh; tampered JWT rejected; compat (personal token + JWT both authorize).

## 2. Console frontend

- [ ] 2.1 Login page (antd form) at /console/login; 401 handler redirects there; token stored (access in memory, refresh in localStorage).
- [ ] 2.2 Header: operator name from JWT claims; logout → refresh revoked.
- [ ] 2.3 First-login forced password change; self-change with old password.
- [ ] 2.4 Machine-token paste moves to an "advanced" section (kept for evals/scripts).
- [ ] 2.5 E2E: login → page → logout; wrong password; refresh flow.

## 3. Docs

- [ ] 3.1 ADR-0010 (JWT auth, revises ADR-0008 no-password decision; authored with this change).
- [ ] 3.2 deploy README: QIUQIU_JWT_SECRET, login flow, director account creation.
- [ ] 3.3 operator-control evals green (they use APP_TOKEN machine path — unaffected).

## Sequencing

After intent-router (no code dependency, but same branch trains are cleaner). ~2 days total.
