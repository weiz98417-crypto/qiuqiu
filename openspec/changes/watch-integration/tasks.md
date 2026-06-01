# Watch Integration 任务拆解

### T1: WebSocket 服务
- `/ws/match/{match_id}` 端点
- 客户端连接管理（连接池）
- 心跳保活 + 自动重连
- 二进制帧（PCM 音频） + 文本帧（JSON 消息）
- **验证**: 客户端连接、心跳、断连重连全通路

### T2: 主循环串联
- `WatchSession.Run()` 主循环
- event-engine 的 chan → 主循环消费
- 调用 ai-pipeline（LLM + TTS）
- 更新 live2d character 表情
- 冷却管理
- **验证**: 单事件 → 端到端听到语音

### T3: 会话状态机
- waiting/watching/paused/ended 状态转换
- 会话生命周期管理（session_id, started_at, metrics）
- 暂停/恢复
- **验证**: 完整状态转换路径测试

### T4: 异常处理
- 数据源断连检测 + 指数退避重连
- LLM 超时 → 自动降级
- TTS 失败 → 文字气泡降级
- WebSocket 断连 → 语音暂存 + 重连补发
- 比赛延期/取消 → 提示 + 比赛列表
- LLM 安全拦截 → 静默丢弃 + 重试
- TTS 全静音检测 → 降级
- **验证**: 每种异常场景触发 + 恢复

### T5: 比赛切换
- 切换确认弹窗（Flutter 侧）
- 会话终结 + 新会话创建
- 数据源切换
- **验证**: 切换比赛 < 3s 完成

### T6: 数据埋点
- session_start/end, event_trigger, ai_reply, error_occurred
- 每个事件关键的 latency_ms
- **验证**: 完整陪看后所有埋点事件可查

### T7: 集成测试
- event-engine → ai-pipeline 串联测试（模拟真实比赛事件注入）
- 端到端测试：api-sports mock → 事件管道 → LLM → TTS → WebSocket → 客户端
- 异常跨层传播测试（api-sports 500 → 用户看到什么）
- Go race detector（`go test -race`）在 CI 中开启
- **验证**: 90 分钟模拟比赛无 goroutine 泄漏，内存增长 < 20MB
