# Audio Playback — 客户端音频播放模块

## 目标

实现 Flutter 端 PCM 音频流的接收、缓冲、播放、音频焦点管理，保证用户在陪看场景下听到完整的球球语音。

## 范围

| 模块 | 内容 |
|------|------|
| PCM 音频接收 | WebSocket 二进制帧 → PCM 数据提取 |
| 播放引擎 | PCM 24kHz mono → 系统音频播放 |
| Jitter Buffer | 音频抖动缓冲（200ms 缓冲窗口） |
| 音频焦点 | 来电话/切后台/其他 App 播放时的处理 |
| 播放状态回调 | 通知 Live2D Widget 说话开始/结束 |

## 不在范围

- TTS 合成（ai-pipeline）
- Lip Sync 计算（live2d-character，WebView 内闭环）
- 用户语音录制（Phase 2 ASR）

## 依赖

- `phase-0-foundation` — Flutter 脚手架、WebSocket 连接

## 里程碑

- [ ] PCM 音频流收到后立即播放，无卡顿
- [ ] 来电话时自动静音，挂断后恢复
- [ ] App 切后台时音频继续播放（后台音频模式）
- [ ] 网络抖动时语音连续不丢帧
