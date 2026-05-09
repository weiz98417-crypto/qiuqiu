# Audio Playback 任务拆解

### T1: PCM 播放引擎
- 集成 `flutter_soloud` 依赖
- PCM 16bit 24kHz mono 解码配置
- 流式推送播放
- **验证**: 用预生成的 PCM 文件验证播放

### T2: Jitter Buffer
- Ring Buffer 实现（200ms 缓冲窗口）
- 缓冲不足时静音补偿
- 缓冲溢出时丢旧帧
- **验证**: 模拟网络抖动，播放无卡顿

### T3: 音频焦点管理
- `audio_session` 集成
- 来电/挂断处理
- 后台音频模式
- 静音控制
- **验证**: 播放中来电话 → 静音 → 挂断后恢复

### T4: Live2D 播放同步
- 播放状态 `ValueNotifier<bool>` → Live2dWidget
- 播放开始 → `setSpeaking(true)`
- 播放结束 → `setSpeaking(false)`
- **验证**: 球球说话时 Live2D 嘴型启动，结束时停止
