# Live2D Motion Pack Expansion

## Why

QiuQiu already has a working Live2D runtime and a small action scheduler, but the character still needs more body language to feel like a football companion. The next step is to add new motion assets in a controlled way so every new motion can be previewed, mapped to a football moment, and verified in the browser.

## What Changes

- Define a repeatable workflow for adding `.motion3.json` and optional `.exp3.json` assets.
- Extend the model motion groups in `female_01Arkit_6.model3.json`.
- Extend the runtime action map in `live2d.html`.
- Add a browser-facing action test surface so new motions can be checked quickly.
- Use football companion semantics instead of raw filenames: goal, miss, complaint, analysis, greeting, tense, celebration, calm idle.

## Accepted First Motion Pack

The first motion pack should prioritize actions that make the assistant feel present during a match:

| Action | Product moment | Minimum asset target |
| --- | --- | --- |
| `celebrate` | goal, big save, favorite team scores | 2 motions |
| `miss` | shot wide, shot blocked, almost goal | 1 motion |
| `complain` | bad call, VAR dispute, user complaint | 1 motion |
| `analysis` | tactical explanation, user asks why/how | 1 motion |
| `tense` | dangerous attack, stoppage time, penalty buildup | 1 motion |
| `agree` | short acknowledgement, "yes", user says something reasonable | 1 motion |
| `wave` | greeting, match start, farewell | 1 motion |

The runtime may map these to existing motions as placeholders until true assets are available, but the public action names above should remain stable.

## Non-goals

- Do not replace the `.moc3` model in this change.
- Do not change DeepSeek, ASR, TTS, or match data integration.
- Do not commit API keys or private source credentials.
