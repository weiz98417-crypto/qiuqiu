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
                                       │  ├─ LLM (MiMo)    │
                                       │  └─ TTS (MiMo)    │
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
cp backend/.env.example .env
# 编辑 .env 填入:
#   - APP_TOKEN: 随机生成的服务访问口令（生产环境必填）
#   - ALLOWED_ORIGINS: 用户端和企业控制台的 HTTPS 来源
#   - POSTGRES_PASSWORD: 独立的数据库强密码
#   - MIMO_API_KEY: 对话、语音识别与语音合成统一密钥

# 2. 构建并启动服务（镜像内会自行编译 Flutter Web）
docker compose up -d --build
```

### 本地开发

**后端:**

```bash
cd backend
cp .env.example .env   # 本地可不设置 APP_TOKEN；生产环境必须设置
go run cmd/server/main.go
```

**客户端:**

```bash
cd client
flutter pub get
flutter run \
  --dart-define=QIUQIU_WS_URL=ws://10.0.2.2:8080/ws/match/test \
  --dart-define=QIUQIU_APP_TOKEN=你的服务访问口令
```

由后端提供 Web 页面时，客户端默认使用当前域名的同源 WebSocket；只有 Flutter 开发服务或前后端分开部署时，才需要覆盖 `QIUQIU_WS_URL`。浏览器若拦截首次主动语音，字幕和动作仍会立即出现，第一次触碰页面会继续播放待播语音。用户端不会显示服务端口令或模型配置；企业控制台若启用口令，通过浏览器本地存储键 `qiuqiu.operator.token` 保存，不再把口令放进 URL。

## 对话管道

球球的对话处理管道按以下流程运作：

1. **VAD (Voice Activity Detection)** — 检测用户是否在说话
2. **ASR (Automatic Speech Recognition)** — 将语音转为文字
3. **事件引擎 (Event Engine)** — 融合比赛上下文和用户输入
4. **LLM 生成** — 调用 MiMo 生成自然的陪看回应
5. **TTS 合成** — 通过 MiMo 将文字转为语音
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
