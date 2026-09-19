# Watch Integration 技术设计

## 1. 主循环（事件调度 + 独立处理 goroutine）

```
主循环（只做调度和路由，不阻塞）：
  for {
    select {
      case event := <-s.eventChan:
        if s.cooldownUntil.After(time.Now()) && event.Priority > 0 {
          s.pendingEvents = append(s.pendingEvents, event)  // 冷却中非P0排队
        } else {
          go s.processEvent(event)  // 独立 goroutine，不阻塞主循环
        }
      case <-s.cooldownTimer.C:
        s.processPending()  // 冷却结束，处理积压事件
      case <-ctx.Done():
        return
    }
  }
```

LLM/TTS 在独立 goroutine 中处理，主循环立即返回监听下一个事件。`eventChan` buffer=1024，满时丢弃 P4 事件。

## 2. 会话状态机

```
        ┌──────────────────────────────┐
        │                              │
        ▼                              │
    [waiting] ──match_start──▶ [watching] ──match_end──▶ [ended]
                                   │
                                   ├── user_pause ──▶ [paused]
                                   │                    │
                                   │    user_resume ────┘
                                   │
                                   └── user_switch ──▶ [ended] → 新会话
```

## 3. WebSocket 协议

### 3.1 连接认证

MVP 阶段无用户系统，使用 app-level shared token 认证：
- 客户端连接时携带 `?token={APP_TOKEN}` 参数
- 服务端校验 token，拒绝无效连接
- token 通过环境变量注入，不硬编码

### 3.2 连接安全限制

| 限制 | 值 | 说明 |
|------|-----|------|
| 单 IP 最大连接数 | 5 | 防止单客户端耗尽资源 |
| 消息最大大小 | 64KB | 防止恶意大消息 |
| 读超时 | 60s | 无心跳则断开 |
| 写超时 | 10s | 发送超时断开 |

### 3.3 消息格式

```json
// 服务端 → 客户端
{ "type": "event",   "event": "goal", "data": {...} }
{ "type": "audio",   "format": "pcm", "data": "<binary>", "expression": "excited" }
{ "type": "expression", "state": "excited" }
{ "type": "match_status", "status": "live" }

// 客户端 → 服务端
{ "type": "ping" }
{ "type": "voice", "data": "<binary>" }           // Phase 2
{ "type": "toggle_talkativeness", "to": "quiet" } // Phase 2
```

## 4. 异常处理矩阵

| 异常 | 检测 | 用户感知 | 恢复 |
|------|------|---------|------|
| 数据源断连 | >15s 无事件 | Live2D idle + 提示"信号断了" | 指数退避重连 |
| LLM 超时 | >2s 无响应 | 切换 Ollama 或兜底句式 | 下次调用恢复主方案 |
| LLM 安全拦截 | 黑名单命中 | 静默丢弃，重新生成 | 重试 1 次 |
| TTS 失败 | API 超时/错误 | 文字气泡 + "嗓子不舒服" | 下次调用恢复 |
| WebSocket 断连 | 心跳超时 | 自动重连 + 语音暂存 | 指数退避 1s→2s→4s→8s |
| 比赛延期/取消 | status 变化 | 提示用户 + 比赛列表 | — |
| 全静音 TTS | RMS < 阈值 | 降级文字气泡 | 重试 1 次 |

## 5. 端到端时序（进球场景）

```
0ms      事件到达（进球）
60ms     上下文增强完成
70ms     LLM 请求发出
650ms    LLM 返回文本 ✓
660ms    句式检查通过
665ms    TTS 请求发出 + Live2D 表情=excited
1200ms   TTS 首包 → 客户端开始播放 ← 用户听到
1500ms   播放完成 → 进入 8s 冷却

总延迟：1200ms ✓ (目标 < 2s)
```

## 6. 比赛切换流程

```
1. 用户点击切换 → 确认弹窗
2. 当前会话标记 ended
3. 发送 goodbye 语（可选，仅切换前 1s 内无P0事件时）
4. 断开旧 WebSocket → 保存会话记录
5. 建立新 WebSocket → 新会话开始
6. Live2D 从 idle 重新激活
7. 球球打招呼："换一场看看～{对阵}"
```

## 7. 目录结构（Go 后端）

```
backend/internal/
├── ws/               # WebSocket handler
├── session/          # WatchSession 生命周期
├── pipeline/         # 主循环（串联 event/llm/tts/live2d）
├── error/            # 异常处理 + 降级决策
├── telemetry/        # 埋点
└── config/           # 配置加载
```
