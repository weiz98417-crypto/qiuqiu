# AI Pipeline 任务拆解

### T1: LLM Prompt 系统
- System Persona + Style Rules + Safety Rules 文本定稿
- 场景 Prompt 模板（goal/shot/card/start/end）
- Prompt 组装函数（8 层拼接，token 预算控制）
- Prompt 文件管理（prompts/v1.0/ 目录）
- **验证**: Prompt token 数 < 1000

### T2: LLM 调用 + 降级
- Deepseek API client（兼容 OpenAI 格式）
- Ollama client（本地备选）
- 主→备自动切换（<500ms 检测）
- 兜底句式模板（所有事件类型覆盖）
- **验证**: 20 次连续调用，生成内容风格一致；模拟 API 不可用，确认切到 Ollama

### T3: 输出校验
- 长度/格式/安全底线校验
- 语义相似度检测（simhash + Hamming 距离，与最近 10 条对比，>0.8 则重试）
- 重试逻辑（加大 temperature，最多 3 次）
- **验证**: 20 条输出无重复句式，0 条触犯安全底线

### T4: TTS 流式合成
- ElevenLabs API 封装
- 流式输出（边合成边推流）
- 句子边界检测（best-effort，超时 2s 强制截断）
- 情绪参数映射
- **验证**: 首包 < 800ms，5 种情绪可感知区分

### T5: 降级链路串联
- LLM 降级：Deepseek → Ollama → 句式模板
- TTS 降级：ElevenLabs → 文字气泡
- 音频缓存（高频固定内容）
- **验证**: 模拟各层故障，确认降级生效
