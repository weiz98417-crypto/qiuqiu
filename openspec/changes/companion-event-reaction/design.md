# Design

## Reaction Layers

Each event may drive four layers:

- Motion: Live2D motion or runtime body pose.
- Expression: face/expression state when available.
- Effect: PIXI or DOM effect layer.
- Speech: optional short proactive line or queued chat suggestion.

## Priority

Suggested priority order:

1. `goal`, `penalty`, `red_card`
2. `big_chance`, `save`, `var_check`
3. `shot`, `miss`, `yellow_card`, `injury`
4. `pressure`, `tactical_shift`, `substitution`
5. `operator_note`

Higher priority reactions may interrupt lower priority idle/listening motions. They should not interrupt active user message handling unless the event is a major match moment.

## Cooldown

- Major reactions: minimum 8 seconds between full-screen/high-energy effects.
- Medium reactions: minimum 4 seconds.
- Minor reactions: can be quiet or expression-only.

## Event Mapping

Initial mapping:

| Event | Action | Effect Intent |
| --- | --- | --- |
| `goal` | `celebrate` | goal burst |
| `big_chance` | `tense` | pressure pulse |
| `save` | `surprise` or `proud` | shock/save flash |
| `miss` | `miss` | near miss |
| `foul` | `complain` | referee dispute |
| `yellow_card` | `complain` | warning card |
| `red_card` | `angry` | intense card |
| `var_check` | `tense` | review frame |
| `penalty` | `tense` | heartbeat |
| `tactical_shift` | `analysis` | tactics board |
| `pressure` | `focus` | focus ring |
| `halftime` | `analysis` | calm recap |
| `fulltime` | `comfort` or `celebrate` | result dependent |

## Voice

For A-user positioning, speech should be casual and companion-like:

- Explain without sounding like a commentator.
- Ask the user what they think after big moments.
- Avoid pretending to have seen events that were not in match state.
