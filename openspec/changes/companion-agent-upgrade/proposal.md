# Companion Agent Upgrade

## Why

QiuQiu currently uses deterministic routing and structured match memory tools. That is the right safety baseline, but it is not yet a full companion agent. Users will ask follow-up questions, mix smalltalk with match facts, refer to prior turns, and expect QiuQiu to answer naturally without inventing unrecorded events.

The next architecture step is to harden the agent boundary while keeping the current factual tool model. The first implementation should prefer an in-process Go boundary unless orchestration complexity clearly requires a separate TypeScript service.

## What Changes

- Define the companion agent as an explicit orchestration boundary.
- Keep Go as the realtime fact, WebSocket, and operator API layer.
- Implement the first upgraded boundary in Go, or document the decision if a TypeScript service is introduced.
- Keep TypeScript agent service migration as a later option behind stable DTOs and tool schemas.
- Formalize tool schemas for match snapshot, event search, player timeline, conversation turn writes, trace writes, and response emission.
- Add short-term conversation memory for follow-up questions.
- Add guarded LLM generation that cannot override structured facts.
- Extend evals for follow-up context, missing facts, control commands, and trace completeness.

## Non-goals

- Do not allow the user-facing agent to create or correct match facts.
- Do not replace structured match queries with vector search.
- Do not remove deterministic fallbacks.
- Do not build long-term personalization beyond basic preference storage.
- Do not introduce a separate TypeScript runtime unless the Go boundary cannot satisfy the evals.

## Success Criteria

- QiuQiu can answer factual match questions from tools and refuse to invent missing facts.
- QiuQiu can handle simple follow-ups like "who passed it?" after an assist question.
- Smalltalk and control commands route away from match memory.
- Every reply writes a trace with intent, tools, retrieved facts, output, reason, latency, and error state.
- LLM failures fall back to safe deterministic replies.
