## 1. Trace API

- [x] 1.1 Define trace response DTOs for list and detail views.
- [x] 1.2 Add `GET /api/matches/{matchId}/traces` behind operator token auth.
- [x] 1.3 Add `GET /api/matches/{matchId}/traces/{traceId}` behind operator token auth.
- [x] 1.4 Support PostgreSQL-backed trace reads from `agent_traces`.
- [x] 1.5 Return an empty trace list safely when trace storage is unavailable.

## 2. Trace Persistence Contract

- [x] 2.1 Verify Companion Explorer writes input, intent, tool calls, retrieved event IDs, output, reason, and timestamp.
- [x] 2.2 Add latency and error fields to trace writes if missing.
- [x] 2.3 Ensure trace records never include API keys, tokens, or provider secrets.

## 3. Operator UI

- [x] 3.1 Add a trace/log entry point in the operator console.
- [x] 3.2 Build a compact trace table for recent decisions.
- [x] 3.3 Build a trace detail drawer or panel.
- [x] 3.4 Add filters for missing facts, proactive lines, user questions, and errors.
- [x] 3.5 Show retrieved event IDs as copyable text.

## 4. Evals And Tests

- [x] 4.1 Add backend API eval for trace list access with valid token.
- [x] 4.2 Add backend API eval for unauthorized trace access.
- [x] 4.3 Add backend API eval that a user question produces a readable trace.
- [x] 4.4 Add UI smoke test or browser QA for opening the trace panel.

## 5. Documentation

- [x] 5.1 Document how operators should use the trace viewer.
- [x] 5.2 Document trace fields and policy labels.
- [x] 5.3 Update the companion architecture doc with the trace viewer role.
