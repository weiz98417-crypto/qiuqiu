# Design: Operator Console

## Locked decisions (two grilling rounds)

| # | Decision |
| --- | --- |
| 1 | Identity = `operators` table (name / token hash / role / scopes); personal tokens, bootstrap from `QIUQIU_BOOTSTRAP_OPERATOR` (`name:token`) when table empty; revocation = row deletion (immediate); audit rows keep the name. No passwords. |
| 2 | Scopes enforced per-route: `ScopeOperatorMatchWrite` / `FactConfirm` / `FactCorrect` / `TraceRead`; roles `director` (all) + `auditor` (TraceRead only). |
| 3 | New app `console/` — Vite + React 19 + TypeScript + antd v6 + React Router + npm; build served by Go at `/console`; dev via Vite proxy; old `/operator.html` coexists until parity (`#live` survives as an external link permanently until its own rewrite wave). |
| 4 | Three-tier IA: 全局层 overview → 比赛层 match/:id → 用户层 …/user/:uid; sources + automation + traces migrate into match layer (期2); monitor cells → global layer (期1/2); `#live` linked (期3). |
| 5 | Style: `linear.app` DESIGN.md structure (awesome-design-md) with the verified QiuQiu palette from `app_theme.dart` — night canvas, orange FF6B35 primary accent, skyBlue secondary, hairline layering; mapped to antd ConfigProvider dark tokens. |
| 6 | Privacy red lines: talkativeness read-only for operators; portrait on-behalf = entries read + delete (confirm + audit + no export). |
| 7 | No pending-proactive approval queue (Non-goal). |

## Backend: identity (migration 042)

```sql
operators(id, name UNIQUE, token_hash, role CHECK IN ('director','auditor'), scopes TEXT[], created_at, revoked_at)
operator_audit(id, operator_name, action, object, created_at)  -- console writes append (operator, action, object)
```

- Token verification: SHA-256 hash lookup per request (no cached sessions — revocation is immediate). Dev bypass (empty APP_TOKEN, non-production) keeps working for local dev but yields `operator:development` with auditor-only scopes.
- Bootstrap: on startup, if the table has no rows and `QIUQIU_BOOTSTRAP_OPERATOR` (`name:token`) is set, seed it; log once.
- All existing operator routes gain a scope check matching their write class (fact writes → FactConfirm/FactCorrect; match/automation/events writes → MatchWrite; reads → TraceRead). The 28 existing operator-control evals must stay green — scopes default `director` tokens to all four.

## Backend: console routes (`cmd/server/console_api.go`)

- `GET /api/console/overview` — aggregate: active matches (store), online sessions + per-match user count (connection registry), memory health (`memoryQueue` degraded flag + backlog depth + last 5 audit rows), thread aging buckets (open by days), recent proactive turns (traces filtered by `proactive_citation:` prefix, cross-match, limit 10).
- `GET /api/console/matches/{id}/users` — per-connection user list joined with `user_preferences.talkativeness`, open-thread counts, portrait overlay `updated_at`.
- `GET /api/console/threads?userId=&state=` + `PATCH /api/console/threads/{id}` `{action:"address"|"expire"}` — the ThreadStore methods already exist; writes go through the idempotency service and append an operator-attributed audit row.
- Traces API: add `citation=` query filter (prefix match on ReasonCodes).
- Memory health read-only accessors on the queue (degraded, backlog depth, audit tail) — no writes exposed.

## Console app structure

```
console/
  DESIGN.md                 ← linear.app structure, QiuQiu palette (from app_theme.dart)
  index.html  vite.config.ts  package.json (npm)
  src/
    main.tsx  App.tsx (React Router: /console, /console/match/:id, /console/match/:id/user/:uid, /console/operators)
    api/ (token storage, fetch helpers with operator token + idempotency keys)
    pages/Overview.tsx  pages/Match.tsx  pages/User.tsx  pages/CitationAudit.tsx  pages/Threads.tsx  pages/Operators.tsx
    theme/ (antd ConfigProvider dark tokens from the palette)
```

- Token UX identical to today (paste once into localStorage) but each token is personal; Operators page (director-only) lists operators, creates (token shown once), revokes.
- Vite dev: proxy `/api` and `/ws` to the Go backend port.

## E2E (new specs, mirroring operator-control idioms)

- token persistence + Operators page create/revoke (director token);
- overview renders the five cells from a booted eval backend;
- citation filter on traces;
- threads: list → mark addressed → backend state asserted; expire path.
- Legacy operator-control specs stay green (scopes default director/all).

## Risks

- antd v6 + React 19 on the served build — verified by `flutter`-style local build + the new console E2E; the Go server only serves static files so deployment shape is unchanged.
- Scope enforcement touching 28 existing operator evals — director tokens default to all scopes; expected green, watch for tests using raw tokens as auditor accidentally.
