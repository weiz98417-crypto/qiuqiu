# Operator Trace Viewer

## Why

Companion Explorer now writes decision traces for user-initiated QiuQiu replies, but operators cannot inspect those traces from the director console. Without a trace viewer, debugging hallucinations, missing facts, intent mistakes, correction handling, and talkativeness decisions requires digging through backend logs or the database.

The director backend should expose QiuQiu's decision process as an operational surface: what the user asked, how the intent router classified it, which match memory tools were called, which event IDs were used, and why the final reply was produced.

## What Changes

- Add an operator-facing trace viewer page or panel in the existing director console.
- Add backend API endpoints for listing and inspecting agent traces by match.
- Show trace details in a scan-friendly table and detail drawer.
- Link trace rows to match event IDs where possible.
- Surface policy labels like missing-fact response, deterministic companion policy, proactive event line, and correction-aware memory.
- Keep trace data read-only in the operator UI.

## Non-goals

- Do not allow operators to edit trace records.
- Do not expose traces publicly to normal users.
- Do not introduce pgvector search in this change.
- Do not rebuild the whole operator console navigation.
- Do not store API keys, tokens, or secrets in trace records.

## Success Criteria

- An operator can open a trace view for the current match.
- The operator can see recent QiuQiu decisions with input, intent, output, and timestamp.
- The operator can inspect tool calls and retrieved event IDs for a single trace.
- Missing-fact answers are visibly distinguishable from normal factual answers.
- Trace API access is protected by the existing operator token.
- Existing match event publishing and user chat flows continue to work.
