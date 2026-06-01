# Runtime Bringup 设计说明

## 后端配置

运行时配置来自 `backend/.env`：

```text
APP_TOKEN=qiuqiu-dev-token
DEEPSEEK_API_KEY=<local secret>
DEEPSEEK_BASE_URL=https://api.deepseek.com/v1
DEEPSEEK_MODEL=deepseek-v4-flash
ELEVENLABS_API_KEY=
REDIS_ADDR=
PORT=8080
```

`DEEPSEEK_API_KEY` 只保存在本地 `.env`，不写入 OpenSpec、README 或代码。

## DeepSeek 请求

`internal/llm.Client` 从配置读取模型名，并对 Chat Completions 请求加入：

```json
{
  "thinking": { "type": "disabled" }
}
```

原因：陪看回复需要低延迟、短句输出。禁用 thinking 后，`deepseek-v4-flash` 直接返回 `message.content`，避免只返回 reasoning 内容导致业务层拿到空回复。

## Live2D 资源

模型来自 Textoon 示例：

```text
live2d-chatbot-demo/public/assets/20250417-200114_model/
```

项目内目标路径：

```text
client/assets/live2d/models/qiuqiu/
  female_01Arkit_6.model3.json
  female_01Arkit_6.moc3
  female_01Arkit_6.4096/
    texture_00.png ... texture_08.png
```

## 本地工具链

当前机器没有系统级 Go/Flutter/Docker PATH。后端用便携 Go：

```powershell
backend/../.tools/go/bin/go.exe
```

Go 依赖下载使用：

```powershell
$env:GOPROXY='https://goproxy.cn,direct'
```

## 验证路径

1. 编译后端：

```powershell
cd backend
..\.tools\go\bin\go.exe build -o qiuqiu-backend.exe ./cmd/server
```

2. 启动后端：

```powershell
.\qiuqiu-backend.exe
```

3. 验证：

- `http://localhost:8080/health`
- `http://localhost:8080/app.html`
- `http://localhost:8080/live2d.html`
- WebSocket `ws://localhost:8080/ws/match/test?token=qiuqiu-dev-token`

## 当前可用入口

浏览器版 MVP 已接入：

```text
http://localhost:8080/app.html
```

能力：

- 展示 Live2D 角色。
- 显示 WebSocket 连接状态和比赛状态。
- 输入文本并发送 `user_speech`。
- 展示 DeepSeek 返回的 `qiuqiu_reply`。
- 回复时触发 Live2D `chat` 表情、说话嘴型和动作。

当前限制：

- 文字对话已通，浏览器麦克风语音到 ASR 尚未纳入此 MVP。
- TTS 未配置 key，因此没有真实音频播放。
- 比赛数据源未配置 key，因此当前使用测试 match id 和基础状态。
