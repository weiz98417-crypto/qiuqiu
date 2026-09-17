# Operator Console: From Match Director Tool to Operations Console

## Why

The companion side gained memory, citation-gated proactivity, talkativeness tiers, portrait privacy ops, and a routed presentation system — but the operator console (a 4,076-line single HTML, zero build tooling, shared `APP_TOKEN`, five hand-written tabs) exposes almost none of it. An emission audit (2026-09-17) found seven operator-facing blind spots: proactive citations invisible, Open Thread ledger unoperable, talkativeness tiers hidden, memory health unobservable, portrait privacy-ops absent, delivery interruptions unsurfaced, and 4 declared permission scopes never enforced per-route.

Meanwhile the console's role changes: QiuQiu is a C-end product where **one match serves many users**, each carrying per-user state (threads, talkativeness, portrait, memory). The console must become an operations console — multi-operator identity, three-tier drill-down (global → match → user), full-inventory routing of the seven blind spots.

## What Changes

- **Identity**: `operators` table (name, token hash, role, scopes) — personal tokens, bootstrap via `QIUQIU_BOOTSTRAP_OPERATOR` env when the table is empty, add/revoke from the console (revocation = row deletion, immediate; audit rows keep the name). The 4 declared scopes become per-route enforced. Two roles: `director` (all scopes) + `auditor` (TraceRead only).
- **New console app**: `console/` — Vite + React 19 + TypeScript + antd v6 + React Router, npm, build served by Go at `/console` (Vite dev proxy to the Go backend in dev). Style base: VoltAgent awesome-design-md `linear.app` DESIGN.md structure with the verified QiuQiu palette from `app_theme.dart` (night canvas / orange FF6B35 primary accent / skyBlue secondary), mapped to antd ConfigProvider tokens (dark).
- **Three-tier IA**: 全局层 `/console`（overview: matches, online sessions, memory health, thread aging, recent proactive citations）→ 比赛层 `/console/match/:id`（events, citation audit, per-user grid: tier/threads/portrait-updated）→ 用户层 `/console/match/:id/user/:uid`（portrait read-only, threads ops, interaction history）.
- **New backend routes** (`cmd/server/console_api.go`, operator auth + idempotency writes): `GET /api/console/overview` (aggregate), `GET /api/console/matches/{id}/users` (aggregate), `GET|PATCH /api/console/threads` (list / mark-addressed / expire — writes via the idempotency service, audit rows carry operator name), traces API gains a `citation=` filter parameter, memory health (degraded / backlog depth / audit tail) read-only.
- **Migration**: 期2 moves sources switching + automation policy editing into the match layer; 期3 links (not rewrites) the legacy `#live` director page and adds portrait privacy-ops (on-behalf delete with confirm + audit, no export). After 期3 the legacy page survives only as `operator.html#live`; its rewrite is a separate future wave.

## Non-goals

- **No pending-proactive approval queue** — real-time match reactions lose their value behind an approval gate; the manual channel (`proactive=manual`) covers "director speaks personally", citation audit covers accountability. Explicitly rejected.
- **No rewrite of the `#live` director page** in this change (audio/ASR interaction deserves its own wave); linked, not migrated.
- **No operator write-access to user preferences** — talkativeness is the user's right; console is read-only.
- **No portrait content export** — on-behalf view is limited to entries; delete requires confirm + audit.
- Five-level pause (legacy fiction) converges to three pragmatic levels: single-ability pause / match takeover / route scopes.
- Legacy 39/36 chapter fiction (role zoo, config governance versioning, handover drills) stays fiction unless a later wave pulls it.
