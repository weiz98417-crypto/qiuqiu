# Voice Interaction 技术设计

## 1. 整体架构

```
Flutter 客户端                      Go 服务端
┌─────────────────────┐     ┌──────────────────────┐
│ 麦克风 → VAD → PCM流 │     │                      │
│         │            │ WS  │  Whisper API         │
│         ▼            │────▶│  (流式ASR)           │
│  按住/自由模式        │     │    │                 │
│         │            │     │    ▼                 │
│         ▼            │     │  意图分类 → LLM → TTS│
│  打断检测            │◀────│  回复文本+音频        │
│  (本地实时)          │     │                      │
└─────────────────────┘     └──────────────────────┘
```

客户端做 VAD + 打断检测（本地低延迟）。服务端做 ASR + 回复生成。

## 2. ASR 选型

| 方案 | 延迟 | 中英混合 | 成本 | 用途 |
|------|------|---------|------|------|
| **Whisper API** | ~1s | 支持 | $0.006/min | MVP 首选 |
| SenseVoice 自部署 | ~300ms | 极好 | 免费 | Phase 3 |

MVP 用 Whisper API，Phase 3 换自部署 SenseVoice。

### 2.1 Whisper API 集成

```go
type ASRClient struct {
    apiKey     string
    httpClient *http.Client
}

func (c *ASRClient) Transcribe(ctx context.Context, audio []byte) (*ASRResult, error) {
    // POST https://api.openai.com/v1/audio/transcriptions
    // model: whisper-1, language: zh, response_format: verbose_json
}
```

### 2.2 上下文增强

识别时传入比赛上下文 hints 提升准确率：

```go
hints := []string{
    match.HomeTeam, match.AwayTeam,
    "进球", "射门", "越位", "犯规", "点球", "黄牌", "红牌",
    "会进吗", "谁进的", "比分多少", "还有多久",
}
```

## 3. VAD + 打断检测（Flutter 端）

### 3.1 VAD 参数

| 参数 | 值 |
|------|-----|
| 静音阈值 | -26dB |
| 最短说话 | 300ms |
| 静音超时 | 800ms |
| 最大时长 | 15s |
| 采样率 | 16kHz mono |

### 3.2 打断流程

```
球球播放中 → 检测到用户语音 → 立即：
  0ms:   音频淡出（100ms线性）
  50ms:  停止播放
  50ms:  Live2D → listening
  100ms: 开始接收用户语音
  +用户说话时长
  +ASR识别 ~1s
  +LLM生成 ~1s
  +TTS首包 ~2s
  ────────────────
  用户感知打断延迟: <150ms
  说完整句→听到回复: ~4s
```

打断后不恢复被中断的内容。

### 3.3 频繁打断保护

30s 内打断 >3 次 → 球球："你今天话好多呀～"

## 4. 意图识别

对 ASR 结果快速分类：

```go
func ClassifyIntent(text string) string {
    switch {
    case matchQuestion(text):  return "question"  // 谁进的/比分/多久
    case matchCommand(text):   return "command"   // 安静点/换一场
    case matchPraise(text):    return "praise"    // 好球/不错
    case matchComplain(text):  return "complain"  // 这都不进/裁判
    case matchNonsense(text):  return "ignore"    // 额/嗯/...
    default:                   return "chat"      // 闲聊
    }
}
```

## 5. 对话上下文

```go
type ConversationTurn struct {
    Role    string // "user" | "qiuqiu" | "event"
    Text    string
    Minute  int
}

// 保留最近 6 轮（3 user + 3 qiuqiu）
history := make([]ConversationTurn, 0, 6)
```

## 6. 两种输入模式

### 按住说话
- 用户按住🎤 → 录音开始 → 松开发送
- 适合嘈杂环境、简短发言

### 自由对话
- 点击🎤切换 → 持续收音 → VAD 自动断句 → 再次点击退出
- 适合安静环境、多轮对话

## 7. WebSocket 协议

```
客户端 → 服务端:
{ "type": "user_speech", "text": "这球会进吗", "intent": "question", "mode": "voice" }

服务端 → 客户端:
{ "type": "interrupt" }                              // 检测到用户插话
{ "type": "qiuqiu_reply", "text": "...", "audio": "<binary>", "expression": "normal" }
```
