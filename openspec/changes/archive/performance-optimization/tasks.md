# Performance Optimization 任务拆解

### T1: 流式 TTS 接入主流程
- SentenceSplitter 串入 ai-pipeline
- LLM 流式 token → 第一句立即送 TTS
- 后续句子连续合成
- 2s 超时强制截断剩余文本
- **验证**: 进球场景，首包延迟 < 800ms

### T2: Prompt 压缩
- System prompt 从 ~200 token 压到 ~150
- 场景 prompt 模板精简
- Token 预算从 810 → ~600
- **验证**: 生成质量不下降，风格一致

### T3: HTTP 连接池
- LLM + TTS + ASR client 共用连接池
- Keep-Alive 启用
- **验证**: 连续 20 次调用，无新建连接延迟

### T4: 音频缓存
- Redis 存储 TTS 结果（sha256 文本→音频）
- 高频内容预缓存（开赛/终场/常见感叹）
- **验证**: 缓存命中延迟 < 10ms
