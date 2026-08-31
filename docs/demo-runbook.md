# QiuQiu Delivery Demo Runbook

This runbook is the repeatable local demo path for the football digital human product. It covers the two core lines:

- Director proactive line: director publishes a live event -> QiuQiu receives it -> QiuQiu speaks to the user side -> traces record the decision.
- User active line: user asks by text or voice -> intent routing -> match memory lookup -> QiuQiu answers from director facts -> traces record the evidence.

## Prerequisites

- Backend runs on `http://localhost:8080`.
- Operator token is read from `APP_TOKEN`; local default is `qiuqiu-dev-token`.
- No external sports-data key is required. The director console is the realtime match fact source.
- Text-only demo works without model or voice keys.
- Voice-enabled demo requires `MIMO_API_KEY` in the process environment. Do not write real keys into files.

Optional MiMo environment:

```powershell
$env:MIMO_API_KEY="{MIMO_API_KEY}"
$env:MIMO_MODEL="mimo-v2.5"
$env:MIMO_VOICE="冰糖"
```

## Start Backend

From `backend`:

```powershell
& ..\.tools\go\bin\go.exe run ./cmd/server
```

If port `8080` is occupied, stop the process using that port or start with another `PORT` and set `QIUQIU_BASE_URL` for smoke scripts.

## Seed Canonical Demo

From the repo root:

```powershell
node .\scripts\demo-seed.mjs
```

The seed script resets only local demo match IDs (`test` or `demo-*`) and writes:

- Match: Spain vs Germany
- Clock: `24:10`, first half
- Score: Spain `1-0` Germany
- Event: Pedri goal
- Assist: Fabian
- Pre-assist: Yamal
- Proactive line: `佩德里这一下太关键了，法比安的助攻也很漂亮。`

## Demo URLs

- User app: `http://localhost:8080/`
- Director live: `http://localhost:8080/operator.html?token={APP_TOKEN}#live`
- Pre-match setup: `http://localhost:8080/operator.html?token={APP_TOKEN}#setup`
- Settings: `http://localhost:8080/operator.html?token={APP_TOKEN}#settings`
- Memory and traces: `http://localhost:8080/operator.html?token={APP_TOKEN}#traces`

## Ten-Minute Demo

1. Start the backend.
2. Run `node .\scripts\demo-seed.mjs`.
3. Open the user app and confirm WebSocket connected status.
4. Open the director live console and confirm Spain `1-0` Germany at `24:10`.
5. Publish a goal, pressure, or operator note from the director console.
6. Confirm QiuQiu proactively appears on the user side as a bot reply.
7. Ask QiuQiu: `刚才谁助攻？`
8. Expected reply mentions `法比安` and `亚马尔`.
9. Ask QiuQiu: `谁策动的？`
10. Expected reply uses the prior event and mentions `亚马尔`.
11. Open `#traces`.
12. Show current match memory, retrieved event IDs, tool calls, output, reason, and voice metadata when available.

## Pre-Demo Verification

Text-only verification:

```powershell
node .\scripts\demo-seed.mjs
node .\scripts\demo-smoke.mjs
node .\scripts\voice-ui-smoke.mjs
```

Voice-enabled verification:

```powershell
$env:MIMO_API_KEY="{MIMO_API_KEY}"
node .\scripts\mimo-voice-smoke.mjs
```

`demo-smoke` verifies health, seeded state, director proactive output over WebSocket, user follow-up answer, and trace evidence. `voice-ui-smoke` verifies browser voice affordances without requiring microphone permission. `mimo-voice-smoke` verifies real MiMo TTS and ASR when a key is supplied through the environment.

## Browser Voice Acceptance

1. Open the user app.
2. Click `语音`.
3. Grant microphone permission when the browser asks.
4. Say: `刚才谁助攻？`
5. Confirm the page enters listening, processing, replying, and speaking states.
6. Confirm QiuQiu answers from director facts and mentions Fabian.
7. Confirm audio plays when `MIMO_API_KEY` is configured.
8. Open `#traces` and confirm ASR text, reply text, TTS status or byte count, retrieved event IDs, and trace ID.

Fallback checks:

- Microphone denied: text composer remains usable.
- ASR failure: UI shows speech fallback and WebSocket stays connected.
- TTS failure: text reply remains visible and Live2D returns to a safe state.
- Browser playback blocked: user can click voice again to unlock playback; text reply is still available.

## Trace Explanation

The trace panel is the customer-facing proof that QiuQiu is grounded:

- `input`: user question or director event description.
- `intent`: routing result such as `recent_event_question`.
- `toolCalls`: memory tools used, such as `match.search_events`.
- `retrievedEventIds`: exact director events used.
- `output`: final QiuQiu reply.
- `reason`: deterministic policy, proactive line, realization fallback, or missing-fact policy.
- `voice`: ASR/TTS status, transcript, mime type, and byte count when available.

Trace payloads must not display API keys, authorization headers, raw environment values, or raw audio.

## Product Value Story

QiuQiu turns a live match into an auditable digital human experience. The director console supplies verified match facts; the companion agent routes user intent, reads match memory, and produces grounded replies; MiMo voice makes the interaction feel live; Live2D actions give the digital human visible emotion; traces make every answer explainable.

The same architecture can extend beyond football into live events, guided learning, commerce livestreams, and customer operations where a digital human needs controlled facts, realtime reaction, user dialogue, and auditable output.

## Troubleshooting

- Port conflict: set `PORT` for the backend and `QIUQIU_BASE_URL` for scripts.
- Stale demo state: rerun `node .\scripts\demo-seed.mjs`.
- Old corrupted text: reset with the seed script.
- Microphone denied: refresh the page or reset browser site permission, then use text fallback.
- ASR/TTS unavailable: verify `MIMO_API_KEY` is set only in the current shell environment.
- PostgreSQL mode: set `DATABASE_URL` to use durable match facts and traces; without it, memory mode is valid for local demo.
