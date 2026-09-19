# MVP Extras 任务拆解

### T1: 极简身份
- Flutter 设置页 UI（昵称输入 + 球队选择器）
- SharedPreferences 本地存储
- 球队列表数据源（api-sports.io 热门联赛）
- 启动时读取已存身份
- **验证**: 设置昵称+主队 → 杀进程重开 → 数据保留

### T2: 用户偏好注入
- Flutter 侧 WebSocket 连接时发送用户偏好（服务端校验合法性）
- Go 服务端接收 + 存储到会话上下文
- LLM Prompt 注入 User Context 层（经清洗后注入，用"""包裹隔离）
- **验证**: 设置主队=皇马 → 皇马进球时球球更兴奋
