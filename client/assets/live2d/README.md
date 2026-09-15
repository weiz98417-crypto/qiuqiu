# Live2D Assets Setup

This folder contains the browser Live2D runtime, the QiuQiu Cubism model, and project-authored football companion motions.

## Cubism Runtime

The browser runtime uses Cubism 5 Web assets:

1. `cubismcore/live2dcubismcore.min.js`
2. `live2d-display-bundle.js`
3. `models/qiuqiu/female_01Arkit_6.model3.json`

The backend serves this folder at `/assets/`, plus `/live2d.html`, `/app.html`, and `/test-expressions.html`.

## Lip Sync

`vendor/wlipsync/` vendors wLipSync 1.3.1 (npm `wlipsync`, MIT) plus its example
viseme profile (`profile.bin`). Both `live2d.html` and the native WebView page
in `client/lib/widgets/live2d_view.dart` load it lazily: the page decodes the
TTS bytes pushed from Flutter, wLipSync turns them into viseme weights, and the
mouth parameters are driven from those. If the vendor files are missing or the
audio-worklet is unavailable, the page falls back to an RMS envelope and then
to the legacy random jaw jitter. Re-download pinned versions with
`node scripts/fetch-lipsync-libs.mjs`.

## Project Motion Pack

The first football companion motion pack was created inside this project for the current `qiuqiu` model parameter set:

- `celebrate_01.motion3.json`
- `celebrate_02.motion3.json`
- `miss_01.motion3.json`
- `complain_01.motion3.json`
- `analysis_01.motion3.json`
- `tense_01.motion3.json`
- `nod_01.motion3.json`
- `wave_01.motion3.json`

These files are project-authored compatibility motions, not imported from a third-party model. They use parameter ids already present in the existing QiuQiu motions so they can load without replacing the `.moc3` model.

When adding a new motion:

1. Put the `.motion3.json` file in `models/qiuqiu/motions/`.
2. Register it under `FileReferences.Motions` in `models/qiuqiu/female_01Arkit_6.model3.json`.
3. Add it to `motionGroups` and `actionMap` in `live2d.html`.
4. Test it from `/test-expressions.html`.
