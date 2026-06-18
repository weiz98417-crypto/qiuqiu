# Voice Browser E2E Acceptance

## Why

Backend evals and MiMo smoke tests prove the voice pieces work, but the product is only convincing when a real browser user can speak to QiuQiu, hear QiuQiu answer, and see Live2D respond. This change turns the voice feature from component-ready into demo-ready.

## What Changes

- Verify the real browser microphone flow end to end.
- Confirm MiMo ASR text reaches the companion agent.
- Confirm MiMo TTS audio returns to the browser and plays.
- Confirm Live2D listening, processing, speaking, and fallback states are visible.
- Capture permission denial, ASR failure, TTS failure, and autoplay-blocked fallbacks.
- Add a repeatable browser voice acceptance runbook or automation.

## Non-goals

- Do not add sports data provider integration.
- Do not change director-console match fact authority.
- Do not build mobile app voice permissions.
- Do not require real voice tests in CI when browser microphone permissions are unavailable.

## Success Criteria

- A user can click the voice control, grant microphone permission, ask "刚才谁助攻？", and receive a correct spoken reply.
- The spoken reply references active director facts, including Fabian and Yamal in the canonical demo.
- Live2D visibly enters listening and speaking states.
- Text input remains usable after microphone denial, ASR failure, TTS failure, or audio playback failure.
- The acceptance process can be repeated by a human operator without reading code.
