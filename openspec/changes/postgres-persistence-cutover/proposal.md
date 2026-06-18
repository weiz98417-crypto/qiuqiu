# PostgreSQL Persistence Cutover

## Why

Memory mode is useful for fast demos, but the product cannot become an operational football companion while match facts and trace logs disappear on restart. The project already has PostgreSQL and pgvector-oriented code paths; the next step is to make PostgreSQL the verified durable mode.

The core product promise depends on preserving match truth: events, participants, corrections, user turns, and agent traces must survive service restarts and be queryable later.

## What Changes

- Verify local PostgreSQL startup with the existing `pgvector/pgvector:pg16` service.
- Run the backend with `DATABASE_URL` and confirm PostgreSQL is the active repository.
- Verify durable persistence for match config, players, match events, event participants, conversation turns, and agent traces.
- Verify whether recent conversation turns can be read back for follow-up memory, not only written.
- Add seed/reset scripts that work against PostgreSQL.
- Verify restart durability for event memory and trace viewer.
- Keep memory mode available for development fallback.

## Non-goals

- Do not add vector search in this change.
- Do not move hard facts like score, assists, or corrections to vector retrieval.
- Do not add multi-tenant account management.
- Do not build a production migration platform.
- Do not remove memory mode.

## Success Criteria

- `docker compose up` starts PostgreSQL and Redis for local development.
- Backend runs with `DATABASE_URL` and logs PostgreSQL mode.
- Match event and trace data survive backend restart.
- Conversation turns survive backend restart and have a clear read path for later agent memory.
- The trace viewer reads persisted traces from `agent_traces`.
- The canonical demo seed works in PostgreSQL mode.
- PostgreSQL tests pass when `DATABASE_URL` is configured.
