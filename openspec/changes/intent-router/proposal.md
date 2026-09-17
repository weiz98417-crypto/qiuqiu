# Intent Router: LLM Routing on Keyword Miss

## Why

Live testing (2026-09-18) confirmed the intent classifier — an 18-row keyword table, first-match-wins (agent.go:1827-1890) — cannot understand natural language: "你在干嘛" (no registered pattern) and "明明进了" (colloquial variant outside the 7-token claim list, agent.go:2238) both fall to Unknown and the canned "这句我没接明白" reply (agent.go:1073). A keyword table is a closed set; natural language's long tail is unbounded. Adding keywords is Sisyphean.

Empirical validation with the production MiMo key: `mimo-v2.5` correctly routed "明明进了，裁判瞎了吗" → `fact_claim_persisted` (confidence 0.85) via function calling in 3.7s, where the keyword table returned nothing. All safety guardrails for an LLM-handled unknown path already exist: ForbiddenClaims injection + validation (agent.go:2756-58), fact-language forced-deterministic backstop (agent.go:1075-80), sentence/char caps, anti-repetition, silence gate.

## What Changes

- **Router on keyword miss**: when the keyword table misses, the turn goes to an LLM router (`mimo-v2.5`, function call `route_turn`) returning `{intent, player, team, score, confidence}` with a **suggested natural reply** (single call — intent + wording in one). Router intents map 1:1 onto the existing 12 backend intents — no new intents downstream.
- **Confidence gating**: fact-class intents (MatchClaim/MatchStatus/RecentEvent/PlayerQuestion/FollowUp/Schedule) require confidence ≥ 0.7 to take the deterministic fact path; below threshold → casual realization + observation/memory recorded as before (evidence never lost).
- **Unknown realization (C1)**: non-fact routed intents and true unknowns realize as natural chat via the LLM realizer under the existing guardrails (ForbiddenClaims, fact_language_policy backstop, validateRealizedText).
- **Claim persistence repair (C2)**: claim detection generalized (坚持副词 明明/真的/确实 + 进了/得分 suffixes); when the coordinator holds an active matching observation from the same user+match, the router labels `fact_claim_persisted` and the policy emits a warm deterministic hold dialogue (not the canned unknown reply).
- **Vocabulary funnel (C3)**: Unknown turns open Open Threads (not just question sentences); the operator overview gains a 没接明白率 metric and top unroutable samples.
- **Observability**: every routed turn appends ReasonCode `router:<intent>:<confidence>` and slot fields to the trace; the console citation audit gains a 路由意图 column.

## Non-goals

- The keyword fast path stays verbatim (control commands, literal claims, score regex — high precision, zero latency, free). No keyword table slimming in this change.
- Fact intents that route successfully still take deterministic ledger-grounded replies — the LLM never writes Match Facts (ForbiddenClaims unchanged).
- No new NLU service (Rasa rejected: Python sidecar + training pipeline outweighs one function call); no embedding classifier (pgvector path noted as future alternative).
- `ROUTER_API_KEY` unset (e.g. CI) ⇒ router layer disabled entirely; behavior identical to today (existing evals stay green).
- Router runs a single attempt with a 6s timeout — no retry stacking on the reply latency.
