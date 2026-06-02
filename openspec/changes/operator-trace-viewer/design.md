# Operator Trace Viewer Design

## 1. Product Goal

The trace viewer lets a match operator answer one question quickly:

> Why did QiuQiu say that?

It should feel like an operational debug panel, not an analytics dashboard.

## 2. Information Architecture

Add a fourth operator section or a contextual panel:

- `赛前配置`
- `实战导演`
- `系统设置`
- `球球日志`

For the first version, prefer a compact trace panel inside the existing operator page instead of a separate product area if navigation risk is high.

## 3. Trace List

The list should show:

- time
- user input preview
- intent
- output preview
- reason / policy
- tool count
- retrieved event count
- error state

Recommended filters:

- all
- missing facts
- correction-aware
- user questions
- proactive lines
- errors

## 4. Trace Detail

The detail drawer should show:

- full input text
- full output text
- intent
- reason
- tool calls as structured JSON-like rows
- retrieved event IDs
- latency
- created time
- error

If retrieved event IDs exist, the UI should show them as copyable IDs first. A later change can add click-through event preview.

## 5. Backend API

Suggested routes:

```http
GET /api/matches/{matchId}/traces?token=...
GET /api/matches/{matchId}/traces/{traceId}?token=...
```

List response:

```json
{
  "traces": [
    {
      "id": "trace_...",
      "matchId": "match-1",
      "userId": "user-1",
      "input": "刚才谁助攻？",
      "intent": "recent_event_question",
      "toolCalls": [{"name": "match.search_events", "args": {"limit": "8"}}],
      "retrievedEventIds": ["evt_1"],
      "output": "刚才这球是法比安助攻，亚马尔参与策动。",
      "reason": "deterministic_companion_policy",
      "latencyMs": 12,
      "error": "",
      "createdAt": "2026-06-02T12:00:00Z"
    }
  ]
}
```

## 6. Data Source

Use `agent_traces` as the source of truth when PostgreSQL is enabled.

When running in memory mode, the backend may return in-memory traces from `companion.StoreMemoryTools` if available. If not available, the API should return an empty list rather than failing the operator page.

## 7. Security

Trace endpoints must use the same token behavior as match config/event write endpoints.

Trace payloads should not include:

- raw API keys
- auth tokens
- provider credentials
- private environment values

## 8. Evals And QA

Minimum backend eval:

1. Create match.
2. Create a goal event.
3. Ask a user question through Companion Explorer.
4. Fetch traces through the operator trace API.
5. Assert intent, tool calls, retrieved event IDs, output, and reason are present.

Minimum UI QA:

1. Open operator console.
2. Trigger or seed at least one trace.
3. Open trace view.
4. Inspect a trace row and detail drawer.
5. Confirm no overlapping text or layout collapse at desktop width.
