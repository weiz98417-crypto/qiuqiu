# Design: Intent Router

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | Router model `mimo-v2.5` (empirically validated: correct route + 3.7s), configured via `ROUTER_BASE_URL`/`ROUTER_API_KEY`/`ROUTER_MODEL` env (defaults to MiMo platform + `MIMO_API_KEY` + `mimo-v2.5`); unset key ⇒ router disabled, legacy behavior |
| 2 | Miss turns accept ~3.7s routing latency (user decision) |
| 3 | Keyword fast path unchanged (18 rows verbatim); router only on miss |
| 4 | Single-call shape: router returns `{intent, player, team, score, confidence, reply}` — `reply` is used as the realized text for non-fact intents (after guard validation); ignored for fact intents (deterministic paths own them) |
| 5 | Confidence gate: fact-class intents require ≥ 0.7; below → casual realization + memory observation recorded |
| 6 | Degradation: router error/timeout (6s, single attempt) ⇒ existing keyword-miss behavior (canned reply / deterministic), never a console error |

## Router call

```json
{
  "model": "mimo-v2.5",
  "messages": [
    {"role": "system", "content": "<路由提示词：12 意图定义 + 事实纪律约束 + 少样本>"},
    {"role": "user", "content": "<用户原文>"},
    {"role": "user", "content": "<当前比赛/观察/线程上下文摘要>"}
  ],
  "tools": [{"type": "function", "function": {"name": "route_turn", "parameters": {...}}}],
  "tool_choice": {"type": "function", "function": {"name": "route_turn"}}
}
```

Tool parameters: `intent` (enum of the 12 backend intents), `player`/`team`/`score` slots, `confidence` (0..1), `reply` (natural reply suggestion for non-fact intents). Prompt authored in Chinese, includes 3 few-shot rows (球进了 → fact_claim; 你在干嘛 → smalltalk; 明明进了 → fact_claim persisted) and the constraint "不得虚构比赛事实，只分类不回答".

## Resolution (agent.go miss path, replacing the :1073 canned default)

```
keyword miss
  → router disabled? → legacy canned reply (today's behavior)
  → router call (6s timeout, once)
      → intent ∈ fact class AND confidence ≥ 0.7
          → deterministic fact path (claim hold / ledger answer / schedule) — reply field ignored
      → intent ∈ chat class (smalltalk/emotion/personal_share)
          → realize with the router's reply suggestion through validateRealizedText
      → unknown / low confidence
          → casual realization (C1) + observation/memory recorded (C3)
      → router error/timeout
          → legacy canned reply + one-shot confused/listening (unchanged)
```

Realize gate update: `shouldRealizeUserTurn` accepts routed chat intents and Unknown-with-router-reply.

## Claim persistence (C2)

`isEventClaim` generalized: 坚持/确认副词（明明/真的/确实/千真万确/就是）+ 进了/得分/破门 ⇒ claim (still excluded when first-person or hypothetical). New policy cue `CueClaimPersisted` fires when the observation coordinator holds an active same-user-same-match observation whose content matches — deterministic warm-hold dialogue: "我知道你看到了……我要等慢一点的源确认，一有结果我立刻喊你". Fact status unchanged (single user repeating is not a second source); coordinator dedupe untouched.

## Vocabulary funnel (C3)

Unknown turns open Open Threads (`kind: unroutable`, not limited to question sentences); the operator overview gains 没接明白率 (unknown turns / total turns, rolling) and the top unroutable samples list. Console threads page already lists/filters.

## Tests

- Router client (httptest stub): payload shape, tool_call parse, timeout, error → degraded.
- Resolution: fact intents with confidence ≥0.7 take deterministic paths; <0.7 → casual; unknown → casual realization through validation.
- fact_language_policy backstop: user text with match-fact language + router present ⇒ still deterministic.
- Eval compatibility: no `ROUTER_API_KEY` in the eval env ⇒ router disabled ⇒ existing canned-reply assertions (eval_test.go:426-444) stay green.
- Golden router journeys: 你在干嘛 → natural smalltalk realization without fact tokens; 球进了 → 没同步 hold; 球进了→明明进了 ⇒ persisted warm hold.
