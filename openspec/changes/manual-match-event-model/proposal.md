# Manual Match Event Model

## Why

World Cup live event data is expensive and hard to guarantee for an early product. QiuQiu should not depend on paid real-time feeds to feel useful. A structured manual event model lets an operator turn the live match into reliable product context while keeping a path open for future data providers.

## What Changes

- Define a canonical match event schema used by the operator panel, AI context, and Live2D reaction layer.
- Support manual event input for key football moments: kickoff, goal, shot, save, foul, card, VAR, substitution, injury, tactical shift, pressure, halftime, fulltime.
- Store enough context for AI replies: match clock, team, player, score, event type, emotional intensity, short description, source, correction status.
- Make every event machine-readable so it can drive character action, speech tone, and match memory.

## Non-goals

- Do not integrate a paid real-time provider in this change.
- Do not build the operator UI in this change.
- Do not promise official data accuracy; manual events are operator-authored.
- Do not record API keys, provider secrets, or private credentials.

## Success Criteria

- All downstream changes can reference one shared event contract.
- Manual events can be validated before they enter match state.
- Future provider events can map into the same schema without rewriting the companion layer.
