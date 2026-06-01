# Runtime Bringup - Live2D + DeepSeek 启动闭环

## 目标

把当前项目从“资源缺失、工具链缺失、后端配置硬编码”推进到可以本地启动的 MVP 状态：

- Live2D 页面能从后端加载模型和纹理。
- 后端能读取本地 `.env` 并调用 DeepSeek。
- WebSocket 用户语音消息能触发 LLM 回复。
- DeepSeek 模型可通过环境变量切换，默认使用 `deepseek-v4-flash`。

## 范围

- 恢复 Textoon Live2D 模型纹理资源。
- 修复 Flutter 端明显编译阻塞。
- 修复 Docker 构建入口和资源复制路径。
- 增加 DeepSeek 模型配置项。
- 在本机用便携 Go 验证后端、Live2D 静态资源、WebSocket、LLM 链路。

## 不在范围

- Flutter SDK 安装和完整 Flutter 客户端运行。
- ElevenLabs TTS 配置。
- API-Sports 实时比赛数据配置。
- 生产部署、CI、正式密钥管理。

## 决策

- `DEEPSEEK_MODEL` 默认值设为 `deepseek-v4-flash`。
- 对 DeepSeek V4 请求显式设置 `thinking.type=disabled`，优先低延迟陪看体验。
- 本地 `.env` 不进入版本库，OpenSpec 只记录变量名，不记录密钥。
- 当前机器缺 Go 时使用项目内 `.tools/go` 便携工具链，不改系统 PATH。

## 验收标准

- `GET /health` 返回 `ok`。
- `GET /live2d.html` 返回 200。
- `GET /assets/models/qiuqiu/female_01Arkit_6.4096/texture_00.png` 返回 200。
- `go run ./cmd/latency-test` 对 DeepSeek 调用成功率 5/5，P95 < 3s。
- WebSocket 发送 `user_speech` 后收到 `qiuqiu_reply`。
