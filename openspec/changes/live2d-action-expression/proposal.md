# Live2D Action And Expression Expansion

## Why

The browser MVP can load QiuQiu and reply through DeepSeek, but the character feels too static. Existing Live2D assets already include multiple idle, listening, speaking, thinking, hello, and expression files. The current bridge only exposes a small fixed subset, so the assistant repeats the same motion and loses the feeling of watching a match together.

## What Changes

- Add a small action scheduler around Live2D expressions, motions, and mouth movement.
- Rotate through available idle, listening, and speaking motions instead of always using index 0.
- Map football companion moments to visible actions: greeting, listening, speaking, thinking, cheering, surprise, complaint, and idle.
- Prevent low-priority idle motions from interrupting user-facing speaking or listening states.
- Expose a stable iframe API for the app page and future Flutter WebView integration.

## Non-goals

- Do not replace the Live2D model or add new binary model assets in this change.
- Do not implement TTS audio playback or real match event polling here.
- Do not document API keys or secrets.
