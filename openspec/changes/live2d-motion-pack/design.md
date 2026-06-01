# Design

## Asset Contract

New motions go in:

`client/assets/live2d/models/qiuqiu/motions/`

Use stable lowercase filenames:

- `celebrate_01.motion3.json`
- `celebrate_02.motion3.json`
- `miss_01.motion3.json`
- `complain_01.motion3.json`
- `analysis_01.motion3.json`
- `tense_01.motion3.json`
- `nod_01.motion3.json`
- `wave_01.motion3.json`

New expressions go in:

`client/assets/live2d/models/qiuqiu/expressions/`

Use names that describe the emotion:

- `happy.exp3.json`
- `surprised.exp3.json`
- `frustrated.exp3.json`
- `thinking.exp3.json`
- `tense.exp3.json`

## Model Mapping

Every new motion must be registered in `female_01Arkit_6.model3.json` under `FileReferences.Motions`.

Recommended groups:

- `celebrate`: goal and big chance success
- `miss`: close shot, blocked shot, regret
- `complain`: referee dispute or user complaint
- `analysis`: tactical explanation
- `tense`: dangerous attack or final minutes
- `agree`: short nod for user acknowledgement
- `wave`: greeting or farewell

## Runtime Mapping

`live2d.html` owns the runtime scheduler. New motion groups should be added to `motionGroups`, then mapped through `actionMap` to semantic actions.

The app should only call `performAction(actionName)`, not raw motion indexes. This keeps Flutter/WebView and future match-event code stable even if the underlying asset list changes.

## Motion And Effect Inventory

There are three animation layers:

| Layer | What it can do | Examples | Limitation |
| --- | --- | --- | --- |
| Cubism motion | Drive parameters inside the `.moc3` model | wave, nod, head/body tilt, facial motion | Cannot create bones or body parts that the model does not expose |
| Runtime body pose | Move the rendered model as a whole | jump, bounce, shake, sway, slump | It is screen-space movement, not true skeletal animation |
| PIXI effects | Draw particles above the model | celebration confetti, anger marks, tension pulse, surprise flash | Effects should be short and not cover the character face |

First effect set:

- `wave`: light side sway, no strong particles.
- `wave`: greeting arcs with a small blue-white glow.
- `celebrate`: jump/bounce plus football-themed goal burst.
- `miss`: goalpost and missed-shot ball.
- `complain`: referee card and whistle.
- `analysis`: coach lean plus a compact tactical board overlay.
- `angry`: quick shake plus red manga anger marks.
- `tense`: lean-in plus heartbeat pressure lines.
- `surprise`: quick flash lines.
- `laugh`: bounce plus sparkle.
- `laugh`: bounce plus small laugh text bubbles.
- `proud`: lifted pose plus crown and highlight stars.
- `curious`: head tilt plus question marks.
- `comfort`: soft lean plus hearts.
- `bored`: slump plus ellipsis.
- `focus`: lean-in plus target reticle.
- `agree`: nod plus green check.
- `happy`: smile arcs and green aura.

`performAction(actionName)` remains the public API. Actions may combine Cubism motion, runtime body pose, and PIXI effects.

## First-Pack Action Map

| Action | Preferred mood | Motion group | Priority | Duration | Placeholder before asset exists |
| --- | --- | --- | --- | --- | --- |
| `celebrate` | `cheer` | `celebrate` | 4 | 2800ms | `cheer` |
| `miss` | `sad` or `complain` | `miss` | 4 | 2400ms | `think` |
| `complain` | `complain` | `complain` | 4 | 2400ms | `think` |
| `analysis` | `thinking` | `analysis` | 3 | 3000ms | `think` |
| `tense` | `nervous` | `tense` | 4 | 2600ms | `listen` |
| `agree` | `happy` | `agree` | 2 | 1400ms | `listen` |
| `wave` | `happy` | `wave` | 3 | 2000ms | `hello` |

Priority meanings:

- 1: background idle only.
- 2: acknowledgement and low-stakes listening.
- 3: normal speaking or analysis.
- 4: match events that should be seen immediately.

## Match Event Mapping

The backend and frontend should converge on these semantic action names:

| Event/input | Action |
| --- | --- |
| `goal`, `penalty_scored`, user says "进球" | `celebrate` |
| `shot`, `shot_on_post`, `save`, user says "差点/没进/可惜" | `miss` |
| `var_check`, `foul`, `yellow_card`, user complains about a call | `complain` |
| `dangerous_attack`, late-game pressure | `tense` |
| user asks "为什么/怎么/分析/战术/阵型" | `analysis` |
| match start, first connection, greeting | `wave` |
| short positive acknowledgement | `agree` |

## Intake Checklist

Each imported motion should be checked before registration:

- It is compatible with this model's parameter ids.
- It does not include absolute local file paths.
- It has source/license notes.
- It loads from `/assets/models/qiuqiu/motions/<file>`.
- It has a matching semantic action or is intentionally idle-only.

## QA Surface

Add or update a local test page to expose each action as a button:

- idle
- listen
- speak
- think
- hello
- cheer
- celebrate
- miss
- complain
- analysis
- tense
- surprise
- happy
- angry

Verification should include:

- The page loads without Live2D asset errors.
- Every button triggers a visible action or expression.
- Speaking actions still animate the mouth.
- Idle does not interrupt speaking or high-priority event actions.
