# Watch Integration — 语音陪看核心流程

## 目标

串联事件引擎 + AI 管道 + Live2D 角色，实现"事件→LLM→TTS→Live2D"端到端陪看体验。含会话管理、异常处理、比赛切换、数据埋点。

## 范围

| 模块 | 内容 |
|------|------|
| 主循环 | 事件→LLM→TTS→Live2D 端到端串联 |
| 会话管理 | 会话生命周期、状态机（waiting/watching/paused/ended） |
| WebSocket 服务 | 赛事事件推送 + 音频流下发 + 用户交互 |
| 异常处理 | 网络断连/数据延迟/AI异常/比赛延期/取消 |
| 比赛切换 | 比赛中切换，保存旧会话、建立新会话 |
| LLM 输出异常处理 | 安全底线拦截/格式校验/全静音检测 |
| 数据埋点 | session_start/event_trigger/user_speak/error_occurred 等 |

## 不在范围

- 用户语音输入（Phase 2）
- 话痨调节（Phase 2）
- 多比赛并发支持（Phase 4）
- 用户系统（Phase 4）
- 性能优化（Phase 3）

## 依赖

- `event-engine` — 事件处理
- `ai-pipeline` — LLM + TTS
- `live2d-character` — 角色展示

## 里程碑

- [ ] 从打开 App 到听到球球说话 < 5s
- [ ] 事件→语音延迟 < 2s
- [ ] 连续陪看 90 分钟无崩溃
- [ ] 网络恢复后 10s 内自动重连
- [ ] LLM 输出异常正确降级（0 句触犯安全底线）
