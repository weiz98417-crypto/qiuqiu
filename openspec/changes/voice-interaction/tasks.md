# Voice Interaction 任务拆解

### T1: VAD + 录音（Flutter 端）
- 麦克风权限 + PCM 16kHz 录音
- VAD 检测（-26dB 阈值，300ms 最短，800ms 静音超时）
- 按住说话 / 自由对话 两种模式
- **验证**: 说话时 VAD 正确检测，静音时正确断句

### T2: Whisper ASR 集成（Go 端）
- Whisper API client 封装
- 流式识别接口
- 比赛上下文 hints 注入
- 错误处理 + 重试
- **验证**: 安静环境 20 句测试，准确率 > 95%

### T3: 打断机制
- Flutter 端：播放中检测语音 → 立即淡出停止
- Live2D 切 listening 状态
- 服务端：接收 interrupt 信号 → 丢弃当前 TTS 输出
- 频繁打断保护
- **验证**: 球球说话中用户插话，150ms 内停止

### T4: 意图识别 + 回复路由
- 意图分类器（question/command/praise/complain/chat/ignore）
- 不同意图 → 不同 Prompt 策略
- 比赛数据查询（"谁进的""比分多少"）
- **验证**: 6 种意图各 5 个样本，分类准确率 > 90%

### T5: 对话上下文管理
- ConversationTurn 上下文维护
- 最近 6 轮注入 LLM Prompt
- 话题不跳转（用户追问时保持同一话题）
- **验证**: 20 轮对话不掉上下文

### T6: 文字输入降级
- 观赛页底部聊天框
- 文字 → LLM → TTS → 播放
- **验证**: 打字发送，球球语音回复
