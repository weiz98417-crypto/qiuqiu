## 1. Voice Input

- [x] 1.1 Add browser microphone control and permission state.
- [x] 1.2 Capture audio and send it through the WebSocket protocol.
- [x] 1.3 Verify ASR request formatting with configured ASR key.
- [x] 1.4 Add ASR mock or fallback for local development without keys.
- [x] 1.5 Show text fallback when voice input fails.

## 2. Voice Output

- [x] 2.1 Verify TTS provider configuration through environment variables.
- [x] 2.2 Send synthesized audio to the browser in a documented format.
- [x] 2.3 Unlock browser audio playback after user gesture.
- [x] 2.4 Play TTS audio without blocking text reply.
- [x] 2.5 Fall back to text-only reply when TTS fails.

## 3. Live2D State Sync

- [x] 3.1 Map microphone listening to Live2D listening motion.
- [x] 3.2 Map ASR/agent processing to thinking or focus motion.
- [x] 3.3 Map TTS playback to speaking state and mouth movement.
- [x] 3.4 Prevent idle motions from interrupting listening or speaking.
- [x] 3.5 Verify high-intensity match events still trigger event-specific actions when voice is idle.

## 4. Evals And QA

- [x] 4.1 Voice text path: user speaks, ASR text reaches companion, QiuQiu replies.
- [x] 4.2 TTS path: reply audio plays and Live2D enters speaking state.
- [x] 4.3 Failure path: ASR or TTS error does not break text chat.
- [x] 4.4 Permission path: microphone denial leaves text chat usable.
- [x] 4.5 Browser console has no blocking audio or recorder errors during the voice flow.
