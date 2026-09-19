# Talkativeness Control 任务拆解

### T1: 三档配置 + Engine 集成
- TalkativenessConfig 数据结构
- Engine.SetTalkativeness() 接口
- 优先级过滤 + 冷却调整 + 间隔倍率
- **验证**: 切安静模式 → 非P0事件不触发

### T2: Flutter 三段滑块 UI
- SegmentedButton 安静/标准/活跃
- 切换时通知服务端（WebSocket消息）
- 球球语音确认
- **验证**: 点活跃 → 服务端收到切换 → 冷却变 4s

### T3: 主动说话（活跃模式）
- 定时器检测用户沉默
- 预置内容池随机抽取
- LLM 生成一句话 → TTS → 播放
- **验证**: 活跃模式 3 分钟无互动 → 球球主动说话

### T4: 偏好持久化
- SharedPreferences 存 talkativeness
- WebSocket 连接时发送
- 下次打开 App 默认使用上次选择
- **验证**: 选活跃→杀进程重开→仍是活跃
