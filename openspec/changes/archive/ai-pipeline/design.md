# AI Pipeline 技术设计

## 1. 管道流程

```
AIGenerationInstruction (from event-engine)
    │
    ▼
[Prompt 组装] Persona + Style + Safety + Context + Instruction
    │ (token预算 ~800)
    ▼
[LLM 调用]
    ├── 主：Deepseek-V4-Flash API (~300-500ms)
    ├── 备：Ollama + Qwen 3.6 35B-A3B (本地，~300ms)
    └── 兜底：预置句式模板 (0ms)
    │
    ▼
[输出校验] 长度/格式/安全底线/与比赛相关性
    ├── 失败 → 重试1次 → 仍失败 → 兜底句式
    ▼
[句式去重] 与最近10条的 simhash 相似度 > 0.8 → Temperature↑ 重试（MVP 使用 simhash + Hamming 距离，部署成本为零）
    │
    ▼
[表情→情绪映射] LLM表情 → TTS情绪参数
    │
    ▼
[TTS 调用]
    ├── 主：ElevenLabs API
    └── 兜底：仅文字气泡
    │
    ▼
[流式下发] PCM 16bit/24kHz/mono → WebSocket 二进制帧 → 客户端
```

## 2. Prompt 结构（完整）

```
Layer 1: System Persona (~150 tokens)   — "你是球球..."
Layer 2: Style Rules (~100 tokens)      — "每次≤2句, 自然口语..."
Layer 3: Safety Rules (~80 tokens)      — "不攻击/不政治/不赌博..."
Layer 4: Match Context (~80 tokens)     — 对阵/比分/时间
Layer 5: Event Context (~100 tokens)    — 事件类型/球员/意义
Layer 6: User Context (~50 tokens)      — 主队/话痨偏好
Layer 7: History (~200 tokens)          — 最近3轮对话
Layer 8: Instruction (~50 tokens)       — 本次任务
─────────────────────────────────────────
总计 ~810 tokens → LLM 可在 1s 内处理
```

## 3. LLM 调用实现

```go
type LLMService struct {
    primary  *deepseek.Client    // Deepseek-V4-Flash
    fallback *ollama.Client       // Qwen 3.6 35B-A3B
}

func (s *LLMService) Generate(ctx context.Context, prompt Prompt) (*LLMOutput, error) {
    // 1. 尝试主方案
    output, err := s.primary.Chat(ctx, prompt, maxTokens=80, stream=true)
    if err != nil {
        log.Warn("Deepseek unavailable, fallback to Ollama")
        output, err = s.fallback.Chat(ctx, prompt, maxTokens=80, stream=true)
    }

    // 2. 输出校验
    if !s.validate(output) {
        output = s.retry(ctx, prompt) // 加大 temperature 重试 1 次
    }

    // 3. 仍无效 → 兜底句式
    if output == nil {
        output = s.templateFallback(prompt.Event)
    }

    return output, nil
}
```

## 4. 输出校验

```go
func (s *LLMService) validate(text string) bool {
    if len(text) == 0 || len(text) > 200 { return false }          // 长度
    if containsMarkdownFence(text) || containsJSONFragment(text) { return false }  // 格式
    if matchesBlacklist(text) { return false }                       // 安全底线
    return true
}
```

## 5. TTS 流式处理

```
LLM 输出: "姆巴佩！！第七十八分钟！皇马反超了！！"
         │
         ▼ 检测句子边界（。！？）
         ├── 第一句: "姆巴佩！！" → TTS 立即合成
         └── 第二句: "第七十八分钟！皇马反超了！！" → 接着合成

优势：用户感知延迟 = max(TTS首包, LLM第一句)，而非两者之和。

> **注意**：句子边界检测是 best-effort 优化。若 LLM 输出第一句不含标点，或首句超短（如"好！"），则等待完整输出后再合成。超时 2s 后强制截断当前累积文本。
```

## 6. 情绪语音参数映射

| LLM表情 | 语速 | 音调 | 表现 |
|---------|------|------|------|
| excited | +15% | +10% | 明显兴奋 |
| nervous | +5% | -5% | 略带紧张 |
| normal | 默认 | 默认 | 自然聊天 |
| tease | +10% | +5% | 轻快吐槽 |
| regret | -10% | -10% | 遗憾感 |

## 7. 音频缓存

```go
// 高频固定内容预生成缓存
cacheKey := fmt.Sprintf("tts:qiuqiu_v1:%x", sha256.Sum256([]byte(text)))
if audio, ok := cache.Get(cacheKey); ok {
    return audio  // 直接返回，跳过 TTS
}
// 未命中 → 合成 → 存入缓存
```

缓存内容：比赛开始/结束语、常见感叹（"好球！""哇！"）
