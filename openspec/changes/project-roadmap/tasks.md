# Project Roadmap 任务

## P0: 稳定当前浏览器 MVP

- [x] Live2D 资源恢复并可由后端服务。
- [x] DeepSeek 文字回复接通。
- [x] `/app.html` 提供可用聊天界面。
- [ ] 修复页面中文乱码和文案编码问题。
- [ ] 增加连接状态、发送中、错误提示、重试按钮。
- [ ] 连续 20 轮对话 QA，确认聊天滚动、Live2D 可见、WebSocket 不断。
- [ ] 把 `server.err.log`/`server.out.log` 加入 `.gitignore`。

## P1: Live2D 动作和人设表现

- [ ] 建立 JS 动作调度器，统一管理 `setExpression`、`playMotion`、`setSpeaking`。
- [ ] 随机 idle 动作：`idle_01`、`idle_02`、`idle_03`。
- [ ] 回复时随机 speak 动作：`Speak_01`、`Speak_02`。
- [ ] 用户输入时播放 listen 动作：`Listen_01-1`、`Listen_01-2`。
- [ ] 快捷事件动作映射：开场 `hello`，进球 `cheer`，思考 `Think_01`。
- [ ] 动作防打断策略：说话中不被 idle 覆盖，回复结束后回 idle。
- [ ] 建立表情映射验收表，验证 7 个 `.exp3.json` 的实际情绪。

## P2: 对话记忆和提示词产品化

- [ ] 将 `ConversationContext` 接入 `user_speech` 分支。
- [ ] 保存最近 6 轮 user/qiuqiu 对话。
- [ ] 在 prompt 中加入当前比赛状态、用户喜好、最近对话。
- [ ] 修复 prompt 文件和源码里的乱码内容。
- [ ] 为不同 intent 增加回复模板约束：question、praise、complain、chat、command。
- [ ] 增加 LLM 空回复、超时、401、429 的用户可见降级文案。

## P3: 语音输入和打断

- [ ] 浏览器版增加麦克风按钮和录音权限状态。
- [ ] 使用现有 `live2d.html` VAD 能力或抽出独立 recorder 模块。
- [ ] 音频转 WAV/base64，通过 `user_speech.audio` 发送到后端。
- [ ] 配置 `SILICONFLOW_API_KEY`，验证 SenseVoice ASR。
- [ ] 用户说话时立即触发 Live2D listening 和后端 interrupt。
- [ ] 语音输入失败时回退到文字输入，不阻塞聊天。

## P4: TTS 音频输出

- [ ] 配置 `ELEVENLABS_API_KEY` 和 voice id。
- [ ] 后端将 TTS 二进制帧下发给浏览器。
- [ ] 浏览器播放音频，并将播放状态同步到 Live2D 嘴型。
- [ ] 处理浏览器自动播放限制：首次用户点击后解锁 AudioContext。
- [ ] TTS 失败时显示文字气泡并继续 Live2D 表情，不让体验卡死。

## P5: 比赛数据和事件陪看

- [ ] 配置 `APISPORTS_API_KEY`。
- [ ] 增加比赛选择页面：今日比赛、正在直播、手动 fixture id。
- [ ] 修复 `Poller` 事件 ID 去重策略，避免同分钟同类型事件互相覆盖。
- [ ] 更新比分状态，确保进球事件后的 score 正确。
- [ ] 事件进入 pipeline 后发送文本事件和表情动作。
- [ ] 无比赛数据时提供 mock match 模式，便于演示和开发。

## P6: Flutter 客户端

- [ ] 安装或配置 Flutter SDK。
- [ ] 跑 `flutter pub get`。
- [ ] 跑 Flutter Web 或 Android。
- [ ] 将浏览器 MVP 的交互能力同步回 Flutter `MatchScreen`。
- [ ] 对齐 WebSocket 协议和 Live2D JS bridge。
- [ ] 验证 Android 麦克风权限、WebView 资源加载、音频播放。

## P7: 部署与质量

- [ ] 整理本地启动命令：便携 Go、Docker、Flutter 三套路径。
- [ ] 修复 Docker 镜像内资源路径并验证 `docker compose up`。
- [ ] 增加后端 smoke test：health、assets、WebSocket、DeepSeek mock。
- [ ] 增加浏览器 QA 脚本：打开 `/app.html`、发送消息、检查回复。
- [ ] 密钥管理：`.env.example` 完整，真实 `.env` 永不提交。
- [ ] 记录 release checklist：启动、验证、回滚、日志位置。
