# Stabilize Demo Pack

## Why

The project already has a working browser MVP: user app, Live2D frame, operator console, match event APIs, companion memory, and trace viewer. The next risk is not feature depth; it is demo reliability.

Before adding new architecture, the product needs a repeatable local demo that starts cleanly, seeds known match facts, publishes an event, answers a user question, and shows the trace. This is the shortest path to customer-facing confidence and internal development speed.

## What Changes

- Remove visible browser-console noise that can undermine demos.
- Provide a UTF-8 safe demo seed/reset path for the Spain vs Germany scenario.
- Define how memory-mode demos avoid stale or corrupted events.
- Add a smoke flow that verifies health, operator event publishing, user question answering, and trace visibility.
- Document exact local URLs for user app, operator live panel, pre-match setup, settings, and trace viewer.
- Make demo data deterministic: Pedri scores, Fabian assists, Yamal pre-assists, Spain leads Germany 1-0.
- Ensure local demo setup does not require external API keys.

## Non-goals

- Do not redesign the operator console.
- Do not introduce the TypeScript agent service in this change.
- Do not require PostgreSQL for the first demo pack; memory mode remains acceptable.
- Do not add real ASR, TTS, or sports data provider dependencies.
- Do not store secrets or real API keys in documentation or scripts.

## Success Criteria

- A clean local demo can be started and seeded in under 3 minutes.
- The user app connects to WebSocket and shows QiuQiu without blocking errors.
- The operator console can publish the standard goal event.
- A user question like "刚才谁助攻？" returns Fabian assisted and Yamal helped build the move.
- The trace viewer shows the intent, memory tool call, retrieved event ID, output, and policy reason.
- Re-running the demo seed starts from a predictable state.
