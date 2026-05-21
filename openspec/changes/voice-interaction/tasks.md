# Voice Interaction 任务拆解

- [x] T1: VAD + 录音（Flutter 端）— 麦克风权限 + PCM 16kHz 录音，接入 VADService 状态机
- [x] T2: Whisper ASR 集成（Go 端）— asr/client.go 已实现
- [x] T3: 打断机制 — 后端 read loop 处理 interrupt，cancel LLM，清空 TTS 队列
- [x] T4: 意图识别 + 回复路由 — pipeline/intent.go 已实现
- [x] T5: 对话上下文管理 — pipeline/conversation.go 已实现
- [x] T6: 文字输入降级 — 已推迟（P4 scope）
