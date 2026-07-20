# QiuQiu Evals

This is the release-quality evaluation system for QiuQiu. It measures the complete companion trajectory: director facts, effective match state, proactive delivery, user follow-up, memory tools, trace evidence, voice metadata, and fallback behavior.

## Suites

| Suite | Purpose | Release rule |
| --- | --- | --- |
| `baseline` | Canonical workflows that customers use every match. | All P0 cases pass. |
| `boundary` | Truth, correction, tool-boundary, malformed-input, and resilience checks. | No blocker failure is allowed. |
| `regression` | A frozen reproduction for every accepted incident. | No case may be removed or silently rebaselined. |

Each case in `evals/cases` is versioned JSON. The schema is in `evals/schema/eval-case.schema.json`; the Go loader also validates IDs, suite names, event ordering, and required fields.

## Graders

The offline runner uses deterministic graders for product-critical behavior:

- **truth**: score, correction, known/unknown facts, response anchors, and forbidden stale facts.
- **claim safety**: false user assertions are corrected or held as unverified without entering match facts.
- **trajectory**: intent, required and forbidden tools, retrieved event IDs, and the user-agent mutation boundary.
- **trace**: output, reason, persistence, and decision metadata.
- **voice**: ASR/TTS/playback metadata when a scenario exercises voice.

LLM judges are deliberately not the authority for match facts. When ReplyRealizer is enabled, deterministic fact routes still bypass it; later subjective judges for warmth and concision must be calibrated against human annotations before they can affect release decisions.

All factual reply routes bypass free-form realization entirely. Cross-source contradictions are represented as `snapshot.integrity.status = conflict`; the companion must hold the disputed fact as unverified until operators explicitly keep the accepted fact or adopt a candidate through the conflict-resolution endpoint.

## Commands

From `backend`:

```powershell
go run ./cmd/evals -suite all -out ../artifacts/evals/offline.json
go run ./cmd/evals -suite baseline
go run ./cmd/eval-audit -base-url http://localhost:8080 -match-id test -out ../artifacts/evals/online-audit.json
```

`eval-audit` reads `APP_TOKEN` only from the environment. It never prints a token and should run after smoke or canary traffic to grade production-like traces.

## Regression Intake

1. Preserve the minimal, anonymized match state and trace evidence from a real failure.
2. Add a new immutable `regression.*.json` case before changing behavior.
3. State the expected fact, event IDs, tools, trace reason, and fallback policy.
4. Fix the product only after the new case reproduces the issue.
5. Keep the case in every PR and release run.

## Execution Tiers

- **PR**: Go tests, all offline suites, API/WebSocket e2e, and browser tests with mocked microphone behavior.
- **Nightly**: repeated performance run, PostgreSQL integration, trace audit, and sampled historical trace replay.
- **Release**: real MiMo TTS/ASR smoke, browser playback acknowledgement, and manual microphone permission acceptance.

The release runner reads `MIMO_API_KEY` from the current environment first, then falls back to the untracked `backend/.env` file. It never writes or logs the key.

Evaluation artifacts go under `artifacts/evals/` and are intentionally not committed. Store release artifacts in CI or object storage with the app revision, case version, provider configuration hash, and timestamps.
