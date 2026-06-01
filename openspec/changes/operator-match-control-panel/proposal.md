# Operator Match Control Panel

## Why

If real-time data is replaced by human operation, the operator experience becomes the data pipeline. The operator must be able to enter match events quickly, consistently, and with low cognitive load while the match is live.

## What Changes

- Add an internal operator-facing web screen for controlling a live match.
- Provide fast event buttons, structured fields, score controls, match clock controls, and an event timeline.
- Support correction/undo for bad entries.
- Publish accepted events to the same pipeline used by the frontend companion.

## Non-goals

- Do not design the final visual style in code; Google Stitch will be used for visual exploration.
- Do not build multi-operator permissions in the first version.
- Do not integrate official live data in this change.
- Do not expose the operator panel as a public user surface.

## Success Criteria

- An operator can log a common event in under 3 seconds.
- An operator can correct a mistaken event without breaking the public match state.
- The frontend companion receives operator events without page refresh.
