# Tasks: Operator Console

## 1. 期1 · Identity + scaffold + 全局层 + citation audit

- [ ] 1.1 Migration 042: `operators` + `operator_audit` tables; bootstrap seeding from `QIUQIU_BOOTSTRAP_OPERATOR`; SHA-256 token verification helper.
- [ ] 1.2 Scope enforcement on all existing operator routes (MatchWrite / FactConfirm / FactCorrect / TraceRead); director role = all scopes; dev bypass downgraded to auditor.
- [ ] 1.3 `console/` scaffold: Vite + React 19 + TS + antd v6 + React Router + npm; ConfigProvider dark tokens from the app_theme palette; `console/DESIGN.md` (linear.app structure, QiuQiu palette); Go serves `console/dist` at `/console`.
- [ ] 1.4 Operators page: list / create (token shown once) / revoke (director-only); audit rows appended.
- [ ] 1.5 `GET /api/console/overview`: five cells (matches, sessions+users, memory health, thread aging, recent proactive citations).
- [ ] 1.6 Overview page renders the five cells; each cell drills into 期2 pages or shows detail drawer.
- [ ] 1.7 Citation audit page: traces with `citation=` filter (backend query param added), reason-code labels for the new C2 codes, one-click "why did it speak" detail.
- [ ] 1.8 E2E `tests/evals/console-identity.spec.mjs`: token persistence, wrong-token rejection, operators create/revoke, overview renders five cells.

## 2. 期2 · 比赛层 + 用户层 + threads/talkativeness/memory cells

- [ ] 2.1 `GET /api/console/matches/{id}/users` aggregate (connections + preferences + thread counts + portrait updated_at).
- [ ] 2.2 Match layer page: events stream + citation-filtered audit + users grid (drills into user layer).
- [ ] 2.3 User layer page: portrait read-only, threads list, interaction history (reuses `interaction?userId=`).
- [ ] 2.4 `GET/PATCH /api/console/threads` ops routes (idempotency + operator-attributed audit rows); Threads page: list/filter/mark-addressed/expire.
- [ ] 2.5 Talkativeness tier read-only on the users grid + user layer (no operator write).
- [ ] 2.6 Memory health cells: degraded flag / backlog depth / audit tail on the overview + match layer.
- [ ] 2.7 Sources switching + automation policy editing migrate into the match layer settings section (legacy `#sources`/`#automation` parity).
- [ ] 2.8 E2E `tests/evals/console-threads.spec.mjs`: list → mark addressed → backend state asserted; expire path; users grid renders.

## 3. 期3 · Portrait privacy-ops + delivery reaction + link-out

- [ ] 3.1 Portrait privacy-ops: on-behalf entries read + slot delete (confirm + operator-attributed audit; no export); wired into the user layer page.
- [ ] 3.2 Delivery-interrupted reaction surfaces on the monitor/match layer (delivery-interrupted standalone presentations listed).
- [ ] 3.3 `#live` director page linked from the console nav (external link to `/operator.html#live`); rewrite tracked as a future wave (Non-goal here).
- [ ] 3.4 E2E: portrait delete on-behalf ⇒ next-turn context clean (mirrors C3 eval) + interrupted reaction visible.

## 4. Docs & bookkeeping

- [ ] 4.1 ADR-0008: operator identity (personal tokens, scopes enforced), three-tier IA, ownership rules, no-approval-queue Non-goal (authored with this change).
- [ ] 4.2 Root CONTEXT.md: Operator（运营员）+ Intervention Level（干预级别）terms.
- [ ] 4.3 Legacy operator-control evals stay green through every phase (scopes default director/all); run per phase.
- [ ] 4.4 After 期3 parity audit: document that `operator.html` survives only as `#live` until its rewrite wave.

## Sequencing

期1 → 期2 → 期3, each independently mergeable. 期1 alone delivers identity + the citation-audit "eyes"; 期2 the "hands" (threads ops); 期3 closes privacy-ops and links the legacy director.
