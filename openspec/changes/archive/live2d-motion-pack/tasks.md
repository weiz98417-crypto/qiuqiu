# Tasks

## Phase 1: Asset Intake

- [x] Collect candidate `.motion3.json` assets and confirm license/source.
- [x] Prioritize first-pack actions: `celebrate`, `miss`, `complain`, `analysis`, `tense`, `agree`, `wave`.
- [x] For `celebrate`, collect at least 2 variants to avoid repeated goal reactions.
- [x] Rename assets to the project naming convention.
- [x] Add motion files under `client/assets/live2d/models/qiuqiu/motions/`.
- [ ] Add optional expression files under `client/assets/live2d/models/qiuqiu/expressions/`.
- [x] Record source attribution in `client/assets/live2d/README.md`.

## Phase 2: Model Registration

- [x] Add new motion groups to `female_01Arkit_6.model3.json`.
- [ ] Add new expressions to `female_01Arkit_6.model3.json` if expression files are added.
- [x] Verify all referenced files return 200 through `/assets/models/qiuqiu/...`.

## Phase 3: Runtime Integration

- [x] Extend `motionGroups` in `client/assets/live2d/live2d.html`.
- [x] Extend `actionMap` with `celebrate`, `miss`, `analysis`, `tense`, `agree`, and `wave`.
- [x] Keep placeholders mapped to existing motions until each new motion asset is imported.
- [x] Update `inferAction` in `client/assets/live2d/app.html` for football phrases.
- [x] Update quick-action buttons only when the action improves the real chat workflow.
- [x] Keep `performAction(action)` as the only app-facing API.

## Phase 4: Test Panel And QA

- [x] Add a motion test panel or update `test-expressions.html` to trigger every action.
- [x] Add runtime body poses for `jump`, `shake`, `sway`, `slump`, and `tremble`.
- [x] Add PIXI effects for celebration, anger, tension, and surprise.
- [x] Separate `wave` opening from `celebrate` goal behavior visually.
- [x] Separate `analysis` from `tense` with tactical-board vs pressure-line effects.
- [x] Add companion emotion actions: `laugh`, `proud`, `curious`, `comfort`, `bored`, and `focus`.
- [x] Expand `/app.html` visible action controls beyond the initial 10 actions.
- [x] Polish effects with semantic overlays instead of generic particles.
- [x] Give every visible `/app.html` action a distinct effect or deliberately quiet visual identity.
- [x] Browser-test `/app.html` and the test panel with gstack/browser.
- [x] Check console and network for missing motion/expression assets.
- [x] Capture browser evidence for app flow after importing the motion pack.

## Phase 5: Product Fit

- [ ] Map each match event type to a character action.
- [ ] Tune priority and duration so celebration feels strong but does not block chat.
- [ ] Decide which actions should be rare to avoid repetition.
- [ ] Document the final event/input/action matrix in the design file after browser QA.

## Acceptance Criteria

- [x] A developer can add a new `.motion3.json` by following this OpenSpec without guessing file paths.
- [x] `/app.html` continues to call semantic actions, not raw Live2D motion indexes.
- [x] Missing new assets degrade to existing placeholder motions instead of breaking the page.
- [ ] Browser QA proves that the character remains visible while chat history grows.
- [x] Console/network checks show no missing model, texture, motion, or expression files.
