# 球球 (QiuQiu) — AI 足球陪看助手

球球是一个 AI 驱动的虚拟足球陪看助手，拥有 Live2D 虚拟形象，支持实时语音对话。她能陪你一起看球、聊球，像一个懂球的朋友一样互动。

## 功能特性

- **Live2D 虚拟角色** — 3D 渲染的二次元角色，带表情和动作系统
- **语音对话** — 支持 ASR 语音识别 + LLM 对话生成 + TTS 语音合成，端到端语音交互
- **足球陪看** — 实时比赛事件理解，自然的中文足球解说和闲聊
- **话痨调节** — 可调节的对话频率控制，从安静陪伴到喋喋不休
- **多平台客户端** — Flutter 客户端，支持 Android / iOS / Web

## 技术架构

```
┌─────────────────┐     WebSocket      ┌─────────────────┐
│  Flutter Client  │ ◄──────────────► │   Go Backend     │
│  (Live2D + VAD)  │    Audio stream   │  (Pipeline)      │
└─────────────────┘                    │  ├─ ASR          │
                                       │  ├─ LLM (DeepSeek)│
                                       │  └─ TTS (ElevenLabs)│
                                       └────────┬────────┘
                                                │
                                          ┌─────┴─────┐
                                          │   Redis    │
                                          └───────────┘
```

## 目录结构

```
qiuqiu/
├── backend/                  # Go 后端服务
│   ├── cmd/
│   │   ├── server/           # 主服务入口
│   │   └── latency-test/     # 延迟测试工具
│   ├── internal/
│   │   ├── asr/              # 语音识别客户端
│   │   └── pipeline/         # 对话处理管道 (AI + 事件引擎)
│   ├── prompts/              # Prompt 模板
│   ├── Dockerfile
│   └── go.mod
├── client/                   # Flutter 前端
│   ├── lib/
│   │   ├── screens/          # 页面 (比赛画面等)
│   │   ├── services/         # 服务层 (WebSocket, VAD, 音频播放)
│   │   └── widgets/          # 组件 (Live2D 视图)
│   ├── assets/live2d/        # Live2D 模型和 Cubism SDK
│   ├── android/              # Android 配置
│   └── web/                  # Web 平台支持
├── openspec/                 # 设计文档和变更记录
│   └── changes/              # 各功能模块的设计提案
├── 演进计划文档/               # 详细的演进计划和技术文档
├── docker-compose.yml        # 一键部署配置
└── README.md
```

## 快速开始

### 环境要求

- Go 1.22+
- Flutter 3.4+
- Redis 7+
- Docker (推荐)

### Docker 部署

```bash
# 1. 配置环境变量
cp .env.example .env
# 编辑 .env 填入:
#   - DEEPSEEK_API_KEY: DeepSeek API 密钥
#   - DEEPSEEK_BASE_URL: DeepSeek API 地址
#   - ELEVENLABS_API_KEY: ElevenLabs TTS 密钥

# 2. 启动服务
docker-compose up -d
```

### 本地开发

**后端:**

```bash
cd backend
cp .env.example .env   # 编辑填入 API 密钥
go run cmd/server/main.go
```

**客户端:**

```bash
cd client
flutter pub get
flutter run
```

## 对话管道

球球的对话处理管道按以下流程运作：

1. **VAD (Voice Activity Detection)** — 检测用户是否在说话
2. **ASR (Automatic Speech Recognition)** — 将语音转为文字
3. **事件引擎 (Event Engine)** — 融合比赛上下文和用户输入
4. **LLM 生成** — 调用 DeepSeek 生成自然的陪看回应
5. **TTS 合成** — 通过 ElevenLabs 将文字转为语音
6. **表情动作** — 根据对话内容触发 Live2D 表情和动作

## 话痨调节

通过 `talkativeness` 参数控制球球的主动说话频率：

| 等级 | 说明 |
|------|------|
| 0 | 安静模式 — 只在被叫时回应 |
| 1-3 | 克制 — 关键事件时说话 |
| 4-6 | 正常 — 重要事件和间歇闲聊 |
| 7-9 | 活泼 — 频繁互动和吐槽 |
| 10 | 话痨 — 几乎不间断评论 |

## License

MIT
