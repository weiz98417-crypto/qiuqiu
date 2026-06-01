# Project Roadmap - 球球工程化路线

## 目标

把当前“浏览器版 Live2D + DeepSeek 文字对话 MVP”推进成真正可用的足球陪看助手：

- 能看见角色、能对话、能听到回复。
- 能接入真实比赛数据，并围绕比赛事件主动评论。
- 能用语音输入，支持打断和连续陪聊。
- 能在浏览器 MVP 和 Flutter 客户端上稳定运行。

## 当前基线

已完成：

- Live2D Textoon 模型资源恢复。
- `/app.html` 浏览器版 MVP。
- Go 后端本地运行。
- WebSocket `user_speech -> qiuqiu_reply` 通路。
- DeepSeek `deepseek-v4-flash` 接入，P95 小于 1s。
- OpenSpec 已记录 `runtime-bringup`。

未完成：

- TTS key 未配置，暂无真实语音播放。
- ASR key 未配置，浏览器麦克风到识别未串起来。
- API-Sports key 未配置，暂无真实比赛事件。
- Flutter SDK 未安装，Flutter 客户端未跑通。
- 动作系统、对话记忆、异常恢复、QA 自动化还未产品化。

## 范围

本路线图覆盖 6 条工程主线：

1. 可用浏览器 MVP。
2. Live2D 动作和人设表现。
3. 语音输入与打断。
4. TTS 音频输出。
5. 比赛数据与事件陪看。
6. Flutter 客户端、部署、质量保障。

## 非目标

- 不做用户账号体系。
- 不做付费系统。
- 不做多角色商店。
- 不做复杂后台运营系统。
- 不把任何 API key 写入 OpenSpec、README 或源码。

## gstack 推荐路线

先把浏览器 MVP 打磨到“演示可信”，再补语音和比赛数据，最后迁移到 Flutter。理由是浏览器 MVP 已经跑通后端、DeepSeek、Live2D，是最快闭环；Flutter 和移动端在工具链、权限、音频播放上风险更高，应该在核心体验稳定后推进。

## 验收标准

- 浏览器页面可连续对话 20 轮，角色不遮挡、不丢连接。
- 用户输入一句话后 2s 内看到文字回复，TTS 配置后 4s 内听到声音。
- 进球、黄牌、射门、开场、结束至少 5 类事件能触发不同回复和动作。
- 麦克风输入能完成“说话 -> ASR -> LLM -> 回复”闭环。
- Flutter 客户端至少 Web 或 Android 一个目标平台可运行。
- 核心测试命令和手动 QA 步骤记录在 OpenSpec。
