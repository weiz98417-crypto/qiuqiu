## 1. Architecture Boundary

- [x] 1.1 Make the first upgraded companion boundary explicit inside the Go backend.
- [x] 1.2 Define stable request/response DTOs for a future out-of-process agent.
- [x] 1.3 Define timeout, retry, and fallback behavior for any LLM or future agent calls.
- [x] 1.4 Document the criteria that would justify introducing a TypeScript agent service later.
- [x] 1.5 Keep direct match fact mutation inside the operator backend only.

## 2. Tool Schemas

- [x] 2.1 Specify `match.read_snapshot` input and output.
- [x] 2.2 Specify `match.search_events` input and output.
- [x] 2.3 Specify `match.get_player_timeline` input and output.
- [x] 2.4 Specify `conversation.append_turn` input and output.
- [x] 2.5 Specify `trace.write_decision` input and output.
- [x] 2.6 Specify `response.emit_companion_reply` input and output.
- [x] 2.7 Add tests that the user-facing agent cannot call operator mutation tools.

## 3. Conversation Memory

- [x] 3.1 Store recent user and QiuQiu turns with match ID and user ID.
- [x] 3.2 Load the last 6-10 turns for user questions.
- [x] 3.3 Support simple follow-ups like "who passed it?" after a goal or assist question.
- [x] 3.4 Keep conversation memory separate from match facts.

## 4. Guarded Generation

- [x] 4.1 Keep deterministic templates for score, assist, player, and missing-fact questions.
- [x] 4.2 Add LLM polish only after facts have been retrieved.
- [x] 4.3 Add fallback replies for LLM timeout, empty output, and provider error.
- [x] 4.4 Ensure missing facts produce a "not recorded" answer instead of a guess.

## 5. Evals

- [x] 5.1 Baseline: goal event, assist question, score question, missing-player question.
- [x] 5.2 Follow-up: user asks "刚才谁助攻？", then "谁策动的？"
- [x] 5.3 Boundary: user claims an unrecorded goal and QiuQiu refuses to confirm it.
- [x] 5.4 Control: user says "talk less" and QiuQiu routes to settings/action policy.
- [x] 5.5 Trace: every path writes intent, tools, retrieved IDs, output, reason, latency, and error.

## 6. Documentation

- [x] 6.1 Update companion architecture docs with the agent boundary.
- [x] 6.2 Document why structured tools remain authoritative over LLM text.
- [x] 6.3 Document when pgvector can be used later and when it cannot.
