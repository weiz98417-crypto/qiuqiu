# Expression System 任务拆解

### T1: 表情引擎（Go 端）
- ExpressionConfig 数据结构 + 优先级枚举
- 事件→表情映射表（19种事件）
- 防抖逻辑（1s最小间隔，同分钟<3次）
- 表情强度衰减（每秒-30%）
- **验证**: 进球 → excited 在 200ms 内设置

### T2: 表情过渡（JS/WebView 端）
- ease-out 插值过渡（100-500ms）
- 过渡矩阵实现
- 表情中途切换 → 重定向到新目标
- **验证**: idle → excited 过渡平滑无闪烁

### T3: 基础动作
- 呼吸动画（ParamBreath sin 周期）
- 随机眨眼（2-5s 间隔）
- 歪头（ParamAngleZ，疑惑时触发）
- 点头（ParamAngleX，用户说话时）
- **验证**: idle 状态下有自然呼吸+眨眼

### T4: TTS 情绪联动
- 表情参数 → TTS 语速/音调映射
- ai-pipeline 集成
- **验证**: excited 表情时 TTS 语速 +15%, 音调 +10%
