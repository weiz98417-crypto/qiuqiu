# Companion Event Reaction

## Why

The manual event pipeline only matters if users feel QiuQiu is alive during the match. Operator events should trigger visible body language, effects, and short proactive companion moments without overwhelming chat.

## What Changes

- Consume structured match events on the frontend.
- Map events to Live2D actions, expressions, effects, and optional proactive speech.
- Add cooldown and priority rules so big moments feel big and small moments do not spam the user.
- Extend the existing action repertoire with football-specific event behavior.

## Non-goals

- Do not add new model files unless required by the current Live2D asset plan.
- Do not make every event produce a chat message.
- Do not block user chat while a reaction plays.

## Success Criteria

- A goal event triggers a distinct celebration path.
- A tense/pressure event feels different from analysis or complaint.
- Repeated events do not look identical every time.
- The user can keep chatting while event reactions happen.
