# QiuQiu Companion Agent Boundaries and Evals

This document defines the first reliable architecture boundary for QiuQiu as a match-watching companion.

## Product stance

QiuQiu is a companion, not a professional broadcaster and not an autonomous sports-data oracle.

The live match source of truth is semantically validated director/provider data plus persisted match memory. User messages are claims to verify, never match facts. The agent may be warm, reactive, and emotionally present, but it must not invent goals, assists, score changes, cards, VAR outcomes, or player events that are not present in match memory.

## Storage direction

Use PostgreSQL as the durable operational database.

Recommended image for local/dev is `pgvector/pgvector:pg16`, so vector retrieval can be added later by enabling the `vector` extension without changing the database product.

Initial tables should stay structured:

- `matches`
- `match_players`
- `match_events`
- `event_participants`
- `conversation_turns`
- `agent_traces`

Add vector columns later only for fuzzy retrieval:

- `match_events.embedding vector(...)`
- `conversation_turns.embedding vector(...)`

Do not use vector search for score, current clock, event correction, or "who assisted?" style factual questions. Those must use structured queries.

## Tool boundary

The companion agent gets tools, not database access. Tool outputs are trusted facts; user text and model text are not.

| Tool | Owner | Purpose | May mutate facts? |
| --- | --- | --- | --- |
| `match.read_snapshot` | Go/Postgres fact layer | Read score, clock, period, current teams, recent events | No |
| `match.search_events` | Go/Postgres fact layer | Read recent/key events by type, time, team, or role | No |
| `match.get_player_timeline` | Go/Postgres fact layer | Read events involving one player | No |
| `match.verify_user_claim` | Companion policy | Compare a user score/event claim with validated match memory | No |
| `conversation.append_turn` | Agent service | Store user/QiuQiu dialog turns | No match facts |
| `trace.write_decision` | Agent service | Store intent, retrieval, model output, action, latency | No match facts |
| `response.emit_companion_reply` | Client delivery layer | Send text/audio/action to user side | No |
| `operator.create_event` | Director backend only | Create match facts from director console | Yes |
| `operator.correct_event` | Director backend only | Correct or supersede match facts | Yes |

Hard rule: the user-facing agent cannot call `operator.create_event` or `operator.correct_event`.

Score and event claims are classified as `confirmed`, `contradicted`, or `unverified`. A contradiction may be corrected naturally only when the current snapshot is internally consistent. Missing or conflicting evidence must stay unverified, and the ReplyRealizer is bypassed so it cannot turn uncertainty into a new fact.

Every user-facing fact path bypasses free-form realization: match status, recent events, follow-up questions, player questions, and user match claims. ReplyRealizer is limited to planned non-factual conversation.

The fact layer rejects impossible transitions before persistence, including pre-match goals, goals that do not add exactly one point to the scoring team, non-goal events that change the score, and configured scorers assigned to the wrong team.

Events from different sources are checked against the same effective match state. An incompatible provider event is rejected and sets `snapshot.integrity.status` to `conflict`; while that status is active, QiuQiu must not confidently confirm the disputed score or event. The current implementation clears this sticky conflict only through a match reset. There is not yet an explicit reconcile/resolve-conflict API, so production operations still need that workflow before automatic source ingestion is considered complete.

## Operator trace viewer

The director console includes a read-only `球球日志` view for the active match.

Operators use this view to inspect why QiuQiu answered or spoke:

- `input`: user text, or an explicit proactive-line marker for director-triggered output
- `intent`: deterministic routing result such as recent event, player question, smalltalk, or proactive line
- `toolCalls`: memory tools used before the reply
- `retrievedEventIds`: match event IDs used as factual grounding
- `output`: the final user-facing QiuQiu line
- `reason`: policy label, for example deterministic companion policy or proactive event line
- `claim`: structured user-claim assessment, including certainty, claimed fact, current fact, status, and integrity reason
- `latencyMs` and `error`: operational diagnostics

The trace viewer is deliberately read-only. If a trace reveals a wrong fact, the operator should correct the underlying match event through the live director timeline, then re-check the next QiuQiu decision trace.

## Runtime split

Keep Go as the realtime fact and delivery layer. Add a TypeScript companion-agent service when LLM orchestration grows.

```mermaid
flowchart LR
  Director["Director Console"] --> GoAPI["Go Backend API"]
  User["User Client"] --> GoAPI
  GoAPI --> PG["PostgreSQL + pgvector later"]
  GoAPI --> Agent["TS Companion Agent"]
  Agent --> Tools["Typed Match Memory Tools"]
  Tools --> GoAPI
  Agent --> Trace["Agent Trace Log"]
  Agent --> GoAPI
  GoAPI --> User
```

The TypeScript agent can use OpenAI Agents SDK or LangGraph later, but the first interface should remain deterministic:

1. intent routing
2. memory retrieval
3. context building
4. guarded LLM generation
5. action policy
6. trace logging

## Evals

The minimum product eval is not "can the model chat?" It is "can the whole companion loop preserve match truth?"

Baseline eval:

1. Configure Spain vs Germany.
2. Director publishes a goal at `23:41`: Pedri scores, Fabian assists, Yamal pre-assists, score becomes `1-0`.
3. QiuQiu proactively emits one user-facing line.
4. User asks: "刚才谁助攻？"
5. QiuQiu answers Fabian assisted and Yamal helped build the move.
6. User asks: "现在几比几？"
7. QiuQiu answers Spain `1-0` Germany.
8. User asks: "穆西亚拉刚才进球了吗？"
9. QiuQiu answers that no Musiala goal is currently recorded.
10. Every turn has a trace with intent, tools called, retrieved event IDs, output, and reason.

Boundary evals:

- Missing player facts must produce "not recorded yet", not a guessed answer.
- Control commands like "少说一点" must route to companion settings/action policy, not match memory.
- Corrected events must not remain active in snapshot answers.
- Quiet proactive mode must record the event but emit no speech.
- Manual proactive mode must preserve the director's written line.
- False user score/scorer/team claims must not be echoed as facts.
- Plausible claims without match evidence must remain unverified.
- Hypothetical scores must remain ordinary conversation rather than becoming match facts.
- Inconsistent persisted snapshots must not be used to overrule the user confidently.
- Conflicting artificial/provider events must set match integrity to `conflict` and pause factual confirmation.

Current executable coverage:

- `backend/internal/companion/eval_test.go`
- `backend/internal/matchstate/store_test.go`
- `backend/internal/matchstate/postgres_test.go` when `DATABASE_URL` is set
- `backend/cmd/server/match_api_test.go`
- `backend/internal/pipeline/prompt_test.go`

Local Postgres smoke test:

```bash
cd backend
DATABASE_URL='postgres://qiuqiu:qiuqiu@localhost:5432/qiuqiu?sslmode=disable' go test ./internal/matchstate -run TestPostgresStoreIntegration
```
