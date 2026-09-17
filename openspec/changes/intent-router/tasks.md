# Tasks: Intent Router

## 1. Router client + resolution

- [x] 1.1 `backend/internal/router`: client (stdlib HTTP, tool-call payload per design.md), config env (`ROUTER_BASE_URL`/`ROUTER_API_KEY`/`ROUTER_MODEL`/`ROUTER_TIMEOUT_MS` default 6000; key unset ⇒ disabled), route result struct, unit tests with httptest (payload shape, parse, timeout, error).
- [x] 1.2 Hook the miss path (agent.go:1890): keyword hit ⇒ unchanged; miss ⇒ router (when enabled) → resolution per design.md (fact ≥0.7 → deterministic; chat → realize with router reply through validateRealizedText; unknown → casual realization).
- [x] 1.3 Policy: Unknown default ActAcknowledge → ActChat with casual speech policy (caps 2 句/60 字, ForbiddenClaims on); `shouldRealizeUserTurn` accepts routed chat + Unknown-with-reply.
- [x] 1.4 fact_language_policy backstop verified for routed turns (user text with match-fact language ⇒ deterministic regardless of router suggestion); test.
- [x] 1.5 Degradation: router error/timeout ⇒ legacy canned reply + one-shot confused/listening (unchanged); test with stub router failure.

## 2. Claim persistence repair (C2)

- [x] 2.1 Generalize `isEventClaim`: 坚持副词（明明/真的/确实/千真万确/就是）+ 进了/得分/破门 ⇒ claim (first-person and hypothetical exclusions unchanged); table test with colloquial variants.
- [x] 2.2 `CueClaimPersisted` when the coordinator holds an active same-user-same-match observation; policy emits the warm deterministic hold dialogue ("我知道你看到了……一有结果我立刻喊你"); fact status unchanged (dedupe, no escalation).
- [x] 2.3 Tests: three-peat scenario (claim → hold → insist ⇒ warm hold, never 没接明白); distinct-content claim opens a second observation.

## 3. Vocabulary funnel (C3)

- [x] 3.1 Unknown turns (non-question included) open Open Threads (`kind: unroutable`); TTL and audit as usual.
- [x] 3.2 Operator overview: 没接明白率 (rolling) + top unroutable samples list.

## 4. Observability + evals

- [x] 4.1 Trace ReasonCode `router:<intent>:<confidence>` + slot fields on routed turns.
- [x] 4.2 Console citation audit: 路由意图 column.
- [x] 4.3 Golden router journeys (你在干嘛 / 球进了 / 明明进了 three-peat) with a scripted router stub in the eval harness; eval-compat test: no ROUTER key ⇒ legacy assertions unchanged.

## 5. Docs

- [x] 5.1 ADR-0009 (LLM router architecture, authored with this change).
- [x] 5.2 Root CONTEXT.md: Intent Router（意图路由器）term.
- [x] 5.3 Refresh course chapter 27/31 anchors after implementation.

## Sequencing

1 → 2/3 (parallel) → 4 → 5. Router first: C2's persistence cue and C3's funnel both read from it.
