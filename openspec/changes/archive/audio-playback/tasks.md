# Audio Playback 任务拆解

- [x] T1: PCM 播放引擎 — 集成 flutter_soloud 实际播放，PCM 16bit 24kHz mono → 扬声器
- [x] T2: Jitter Buffer — Queue<Uint8List> 已实现，200ms 缓冲窗口
- [x] T3: 音频焦点管理 — audio_session 已集成，mute/unmute/lifecycle
- [x] T4: Live2D 播放同步 — didUpdateWidget 已调用 setSpeaking()
