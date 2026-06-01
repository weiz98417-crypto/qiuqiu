# Design

## Iframe API

`live2d.html` should keep the existing functions for compatibility:

- `setExpression(name)`
- `playMotion(name)`
- `setSpeaking(boolean)`

It should also expose richer semantic functions:

- `setMood(name)`
- `performAction(action)`
- `playMotionGroup(group, priority)`

The app page should call semantic actions where possible. Older callers can continue using `setExpression` and `playMotion`.

## Motion Groups

The model has these local assets:

- `idle`: `idle_01`, `idle_02`, `idle_03`
- `listen`: `Listen_01-1`, `Listen_01-2`
- `speak`: `Speak_01`, `Speak_02`
- `think`: `Think_01`
- `hello`: `hello`

The scheduler should choose among group members to reduce repetition.

## Priority

Use short mode windows:

- idle: lowest priority, random every 8-14 seconds
- listen: medium priority while the user message is being sent
- speak: high priority while QiuQiu replies
- event actions like cheer/surprise: high priority, then return to idle

This keeps the character active without letting background idle movements cover important moments.
