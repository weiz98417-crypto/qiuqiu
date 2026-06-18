# PostgreSQL Persistence Cutover Design

## 1. Data Ownership

PostgreSQL becomes the durable operational store for match memory.

Tables remain structured:

- `matches`
- `match_players`
- `match_events`
- `event_participants`
- `conversation_turns`
- `agent_traces`

Vectors can be added later through pgvector, but structured facts stay in normal columns.

## 2. Runtime Modes

The backend should support two explicit modes:

- memory mode: no `DATABASE_URL`, good for quick local development
- PostgreSQL mode: `DATABASE_URL` set, required for durable demos and staging

Startup logs should make the active mode obvious.

## 3. Durability Smoke

The minimum persistence smoke test:

1. Start backend with PostgreSQL.
2. Seed Spain vs Germany.
3. Publish the Pedri goal event.
4. Ask a user question that writes a trace.
5. Confirm user and QiuQiu conversation turns are written.
6. Restart backend.
7. Fetch snapshot, events, traces, and recent conversation turns.
8. Confirm the event, trace, and conversation turns still exist.

## 4. Conversation Memory Boundary

The database already has a `conversation_turns` table and trace writer paths may write user/QiuQiu turns. This change should verify that write path and then close the next boundary: reading recent turns back for agent memory.

Required distinction:

- persistence: user and QiuQiu turns are stored durably
- retrieval: recent turns can be loaded by match ID and user ID
- usage: the companion agent can use retrieved turns for follow-up questions

If usage is deferred to `companion-agent-upgrade`, this change must still expose or document the retrieval interface.

## 5. Reset Strategy

Demo reset should be deterministic. Acceptable approaches:

- delete rows for the demo match ID inside a script
- recreate the local database volume for full reset
- use a dedicated test match ID per smoke run

The preferred path is a script-level match reset because it is faster and safer than deleting the whole database.

## 6. Correction Semantics

Corrected events must remain auditable but inactive for factual answers.

Rules:

- original event status becomes `corrected`
- replacement event status becomes `active`
- snapshots and agent memory use active events only
- trace records retain the event IDs they used at decision time

## 7. pgvector Boundary

`pgvector` is a future extension for fuzzy retrieval:

- vague user memory
- similarity over prior conversation turns
- fuzzy event recap

Do not use vector search for:

- current score
- clock
- event correction
- "who assisted?"
- "did player X score?"

Those questions require structured queries.
