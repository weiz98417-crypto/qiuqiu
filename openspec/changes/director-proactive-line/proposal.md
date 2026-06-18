# Director Proactive Line

## Why

The director console is the authoritative match fact source. QiuQiu should not wait passively for the user after important director events. When the director publishes a live event, QiuQiu should proactively react, speak, and preserve enough context for user follow-up questions.

## What Changes

- Harden director event payload structure for proactive QiuQiu output.
- Ensure event time, score, type, team, participants, description, and director line are preserved.
- Emit proactive text and optional TTS audio to the user app.
- Link follow-up user questions to the proactive event.
- Keep event correction authoritative and auditable.
- Add e2e evals for publish -> proactive -> follow-up -> correction.

## Non-goals

- Do not ingest external sports data providers.
- Do not make QiuQiu create match facts from user chat.
- Do not replace director-written proactive lines with ungrounded model output.
- Do not build a multi-match operations center.

## Success Criteria

- Publishing a goal event from the director console makes QiuQiu proactively respond to the user.
- The proactive response can use director-written text or deterministic fallback text.
- User follow-up questions resolve against the same event.
- Corrected events remain auditable but are not used as active facts.
- The full chain is visible in traces.
