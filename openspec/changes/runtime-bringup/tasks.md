# Runtime Bringup 任务

## T1: 恢复 Live2D 资源

- [x] 确认模型来源为 Human3DAIGC/Textoon。
- [x] 下载 `female_01Arkit_6.4096/texture_00.png` 到 `texture_08.png`。
- [x] 验证 `female_01Arkit_6.model3.json` 引用的 9 张纹理全部存在。

## T2: 修复启动阻塞

- [x] 修复 `MatchScreen._handleMessage` 的 Dart `switch` fall-through 编译问题。
- [x] 修复 Docker build context，使镜像能访问 `backend/` 和 `client/assets/live2d/`。
- [x] Docker 构建入口改为 `./cmd/server`。
- [x] Docker 镜像复制 `backend/prompts` 和 `client/assets/live2d`。

## T3: 接入 DeepSeek

- [x] 增加 `DEEPSEEK_MODEL` 配置项，默认 `deepseek-v4-flash`。
- [x] LLM client 使用配置模型名，不再硬编码 `deepseek-chat`。
- [x] DeepSeek V4 请求显式禁用 thinking mode。
- [x] 本地 `.env` 写入 DeepSeek 配置，且不提交密钥。
- [x] 修复 `.env` 编码，避免 dotenv 读取 key 失败。

## T4: 本地后端验证

- [x] 下载项目内便携 Go 工具链到 `.tools/go`。
- [x] 使用 `GOPROXY=https://goproxy.cn,direct` 下载 Go 依赖。
- [x] 编译 `backend/qiuqiu-backend.exe`。
- [x] 启动后端并验证 `/health`。
- [x] 验证 `/live2d.html` 可访问。
- [x] 验证 DeepSeek latency-test：5/5 成功，P95 < 3s。
- [x] 验证 WebSocket `user_speech` 能返回 `qiuqiu_reply`。

## T5: 剩余工作

- [ ] 安装 Flutter SDK 或配置现有 Flutter 路径。
- [ ] 跑 `flutter pub get` 和 Flutter Web/Android 客户端。
- [ ] 连接客户端页面到当前后端并验证完整 UI 交互。
- [ ] 配置 TTS key 后恢复音频输出验证。
- [ ] 配置 API-Sports key 后验证真实比赛数据。

## T6: 浏览器版可用 MVP

- [x] 新增 `/app.html`，提供 Live2D + 聊天输入 + WebSocket 状态。
- [x] 浏览器页面发送 `user_speech` 到后端。
- [x] 浏览器页面展示 `qiuqiu_reply`。
- [x] DeepSeek 回复驱动 Live2D 表情和说话状态。
- [x] 后端默认路由 `/` 跳转或服务可用页面。
- [x] 重启后端并验证浏览器闭环。
