# Design

## Primary Screen

The operator panel should be a dense live-work interface, not a marketing page.

Required areas:

- Match header: home team, away team, current score, period, clock.
- Quick event grid: goal, shot, big chance, save, foul, card, VAR, penalty, substitution, tactical shift, pressure.
- Structured event composer: team, player, event type, clock, intensity, description.
- Timeline: latest event first, correction affordance, status badges.
- Companion preview: shows the action/speech intent that will be sent to QiuQiu.

## Fast Input Rules

- Common events should be one click plus optional short text.
- Score-changing events must require confirmation of score.
- Cards, substitutions, and injuries should allow player names but not block submission.
- Free-text notes are allowed only as `operator_note`, not as unstructured replacement for event type.

## Real-Time Delivery

Events should be broadcast to connected frontend clients through the existing real-time channel if possible. If the current WebSocket layer is too narrow, extend it with a `match_event` message type.

## Safety

The operator panel should avoid accidental public output:

- Draft state is private.
- Publish action is explicit.
- Corrections are visible in the operator timeline.
- Public frontend receives only active/corrected match state, not private drafts.

## Google Stitch Boundary

Stitch owns visual exploration for this screen. Implementation should translate the approved Stitch output into existing project code without copying brittle generated logic blindly.
