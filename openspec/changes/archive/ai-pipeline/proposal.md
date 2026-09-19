# AI Pipeline — LLM对话生成 + TTS语音合成

## 目标

实现"事件指令 → LLM 生成文本 → TTS 合成语音"的 AI 管道，含人设系统和降级兜底。这是球球的"大脑+嗓子"。

## 范围

| 模块 | 内容 |
|------|------|
| LLM 对话生成 | Deepseek-V4-Flash 主方案 + Ollama 本地备选 + 句式模板兜底 |
| 人设 Prompt 系统 | System Persona + Style Rules + Safety Rules + 动态上下文 |
| 句式多样性 | 相似度检测 + 句式池 + Temperature 调整 |
| TTS 语音合成 | ElevenLabs + 流式输出，降级为文字气泡 |
| 情绪语音映射 | 5 种情绪参数（excited/nervous/normal/tease/regret） |
| 降级链路 | LLM 降级 + TTS 降级 + 兜底句式模板 |

## 不在范围

- 事件处理（event-engine）
- 用户语音回复（Phase 2）
- Live2D 渲染（live2d-character）

## 依赖

- `event-engine` — 接收 AIGenerationInstruction

## 里程碑

- [ ] LLM 生成延迟 P50 < 500ms, P95 < 800ms
- [ ] TTS 首包延迟 P50 < 500ms, P95 < 800ms
- [ ] 情绪语音可感知区分（兴奋/平静/遗憾）
- [ ] LLM 不可用时自动切换本地 Ollama
- [ ] TTS 不可用时自动降级为文字气泡
