# Voice Interaction Closure

## Why

The current product can demonstrate text-based companion interaction. A football digital human becomes much more convincing when the user can speak naturally and QiuQiu can answer with audio while Live2D behaves like a speaking companion.

Voice should be implemented after the demo and persistence foundations are stable. It must degrade cleanly to text when ASR, TTS, browser permissions, or ASR/TTS keys fail.

## What Changes

- Add browser microphone UX with permission, recording, processing, failure, and fallback states.
- Send audio through the existing `user_speech.audio` WebSocket path or document a replacement.
- Verify ASR request/response handling with provider configuration and mock fallback.
- Verify TTS audio delivery and browser playback.
- Synchronize listening, thinking, speaking, and fallback states with Live2D.
- Keep text chat fully usable when voice fails.

## Non-goals

- Do not require real ASR or TTS keys for local development.
- Do not add mobile app voice permission support until browser voice is stable.
- Do not change the match fact authority model.
- Do not introduce sports data provider integration in this change.
- Do not block text replies on audio generation.

## Success Criteria

- A user can ask a match question by voice and receive a text reply.
- When TTS is configured, QiuQiu replies with playable audio.
- Live2D enters listening and speaking states at the right time.
- ASR/TTS failures degrade to text without breaking WebSocket chat.
- Browser audio playback unlock behavior is documented and handled.
