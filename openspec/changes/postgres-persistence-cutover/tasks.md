## 1. Local Infrastructure

- [ ] 1.1 Verify `docker compose up` starts backend, PostgreSQL, and Redis.
- [x] 1.2 Verify `pgvector/pgvector:pg16` is used for local PostgreSQL.
- [x] 1.3 Verify backend starts in PostgreSQL mode when `DATABASE_URL` is set.
- [x] 1.4 Document fallback to memory mode when PostgreSQL is unavailable.

## 2. Persistence Coverage

- [x] 2.1 Verify match config persists in `matches`.
- [x] 2.2 Verify player lists persist in `match_players`.
- [x] 2.3 Verify match events persist in `match_events`.
- [x] 2.4 Verify participants persist in `event_participants`.
- [x] 2.5 Verify user and QiuQiu turns are written to `conversation_turns`.
- [x] 2.6 Verify recent turns can be read back by match ID and user ID, or document the missing retrieval interface.
- [x] 2.7 Verify traces persist in `agent_traces`.

## 3. Demo Seed And Reset

- [x] 3.1 Add or adapt the demo seed/reset flow for PostgreSQL mode.
- [x] 3.2 Ensure reset only affects the intended demo match ID.
- [x] 3.3 Ensure seeded Chinese text round-trips without corruption.
- [x] 3.4 Ensure seed can be re-run without duplicate active facts.

## 4. Restart Durability

- [x] 4.1 Publish the canonical goal in PostgreSQL mode.
- [x] 4.2 Ask a user question to create an agent trace.
- [x] 4.3 Verify user and QiuQiu conversation turns were persisted.
- [x] 4.4 Restart the backend.
- [x] 4.5 Verify snapshot, events, traces, and recent conversation turns still include the canonical data.
- [x] 4.6 Verify corrected events are not used as active facts after restart.

## 5. Tests

- [x] 5.1 Run `go test ./cmd/server ./internal/...`.
- [ ] 5.2 Run PostgreSQL integration tests with `DATABASE_URL`.
- [x] 5.3 Add or update tests for persisted trace reads if gaps are found.
- [x] 5.4 Document any tests skipped because Docker or PostgreSQL is unavailable.
