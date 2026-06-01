# Phase 0 技术设计

## 1. 项目结构

```
球球-演进计划/
├── backend/                    # Go 后端
│   ├── main.go                 # 入口，启动 HTTP + WebSocket
│   ├── go.mod
│   ├── Dockerfile
│   ├── internal/
│   │   ├── ws/                 # WebSocket 处理
│   │   ├── event/              # 事件定义（占位）
│   │   └── config/             # 配置加载
│   └── test/
├── client/                     # Flutter 移动端
│   ├── lib/
│   │   ├── main.dart
│   │   ├── screens/
│   │   │   └── match_screen.dart    # 比赛陪看主屏
│   │   └── widgets/
│   │       └── live2d_view.dart     # WebView 封装
│   └── pubspec.yaml
├── docker-compose.yml
├── 演进计划文档/               # 已归档的设计文档
└── openspec/                   # Change 管理
```

## 2. 后端架构（Phase 0 最小版）

```
                    ┌──────────────────┐
                    │   Flutter App    │
                    └────────┬─────────┘
                             │ WebSocket
                             ▼
              ┌──────────────────────────┐
              │       Go API Server      │
              │                          │
              │  /ws/match/{match_id}    │ ← WebSocket 端点
              │  /health                 │ ← 健康检查
              │                          │
              │  goroutine 1: 事件轮询    │ ← 数据源polling
              │  goroutine 2: WebSocket   │
              └──────────────────────────┘
```

- 单进程，无消息队列
- WebSocket 用 `gorilla/websocket`
- 事件轮询用 Go channel 内部传递
- 配置用环境变量（`os.Getenv`）

### 2.1 Go 依赖

| 包 | 用途 |
|----|------|
| `github.com/gorilla/websocket` | WebSocket |
| `github.com/redis/go-redis/v9` | Redis 客户端（Phase 1 启用） |
| `github.com/joho/godotenv` | 本地开发环境变量 |
| `net/http` | HTTP 服务（标准库） |

### 2.2 WebSocket 协议

```json
// 服务端 → 客户端
{
  "type": "event",
  "event": "goal",
  "data": {
    "team": "主队",
    "minute": 78,
    "player": "某某",
    "score": "2-1"
  }
}

// 服务端 → 客户端
{
  "type": "audio",
  "text": "球进了！！某某第78分钟！",
  "expression": "excited"
}

// 客户端 → 服务端（仅心跳 Phase 0）
{
  "type": "ping"
}
```

## 3. Live2D WebView 方案

```
Flutter Widget Tree
  ┌─────────────────────────────┐
  │  MatchScreen                │
  │  ┌───────────────────────┐  │
  │  │  Live2dView (WebView)  │  │  ← WebView 加载本地 HTML
  │  │  ┌─────────────────┐  │  │
  │  │  │  Canvas (WebGL2) │  │  │  ← Cubism Web SDK 渲染
  │  │  │  角色模型         │  │  │
  │  │  └─────────────────┘  │  │
  │  └───────────────────────┘  │
  │  ┌───────────────────────┐  │
  │  │  比分 / 事件文字       │  │
  │  └───────────────────────┘  │
  └─────────────────────────────┘
```

### 3.1 技术选型

- **WebView 插件**: `flutter_inappwebview`（功能最全，支持 JS Bridge）
- **Live2D**: Cubism 5 SDK for Web + `live2d-renderer-lite`（npm 包，简化加载/动画）
- **模型文件**: 本地 assets 加载（打包进 APK）

### 3.2 Flutter ↔ Live2D 通信

```
Flutter (Dart)                  WebView (JS)
  │                                │
  │  evaluateJavascript(           │
  │    "setExpression('excited')"  │
  │  ──────────────────────────▶  │
  │                                │ Cubism SDK 播放表情
  │  postMessage({type:'ready'})   │
  │  ◀──────────────────────────  │
  │                                │
```

- Flutter → Live2D: `evaluateJavascript()` 触发表情/动作
- Live2D → Flutter: `postMessage()` 通知加载状态

### 3.3 验证用模型

Phase 0 使用 Live2D 官方免费示例模型（如 `Haru` 或 `Hiyori`），不做角色定制。

## 4. 延迟测试方案

```
测式流程：
  1. api-sports.io 拉取一个比赛事件
  2. 事件文本 → Deepseek API（LLM 生成一句话）
  3. LLM 输出 → ElevenLabs / CosyVoice 2（TTS 合成）
  4. 记录每个环节的耗时

  端到端延迟 = 事件到达 → TTS 首包到达

  里程碑：< 3s 即可进入 Phase 1
           > 3s 触发闸门，回退到"方案A"（纯文字+语音，推迟 Live2D）
```

## 5. 部署

Phase 0 不需要部署。本地开发环境：
- Go 服务直接 `go run main.go`
- Flutter 通过 `flutter run` 连接模拟器/真机
- Deepseek API / ElevenLabs API 用个人 Key
