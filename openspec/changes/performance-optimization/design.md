# Performance Optimization 技术设计

## 1. 流式 TTS 落地

当前 SentenceSplitter 已实现但未串入主流程。串入后：

```
LLM token 流 → 句子分割器(。！？边界) → 第一句立即送 TTS
                                         → 第二句接着合成
用户感知延迟 = max(TTS首包, LLM第一句生成)，而非两者之和
```

### 集成点

```go
// aipipe.go: Process() 改为流式
func (p *AIPipeline) ProcessStreaming(ctx context.Context, inst *AIGenerationInstruction) <-chan *StreamChunk {
    ch := make(chan *StreamChunk, 8)
    go func() {
        defer close(ch)
        splitter := NewSentenceSplitter()
        for token := range p.llmClient.Stream(ctx, messages) {
            if sentence := splitter.Feed(token); sentence != "" {
                result := p.ttsClient.Synthesize(ctx, sentence, voiceID)
                ch <- &StreamChunk{Text: sentence, Audio: result.AudioData, Expression: inst.Expression}
            }
        }
        // Force flush remaining
        if remaining := splitter.FlushAll(); remaining != "" {
            result := p.ttsClient.Synthesize(ctx, remaining, voiceID)
            ch <- &StreamChunk{Text: remaining, Audio: result.AudioData, Expression: inst.Expression}
        }
    }()
    return ch
}
```

## 2. Prompt 压缩

当前 system prompt ~200 tokens。压缩到 ~150：

```
你是球球，一个陪用户看球的AI语音助手。像朋友一样聊天，不是解说员。
风格：每次≤2句，自然口语，表达情绪+观察，变换说法。可用"哇""哎""好球！"开头。
底线：不攻击球队/球员/裁判，不谈政治/宗教/种族，不鼓励赌博，不用脏话，不做绝对预测。
```

## 3. HTTP 长连接

```go
httpClient: &http.Client{
    Transport: &http.Transport{
        MaxIdleConns:        20,
        IdleConnTimeout:     90 * time.Second,
        DisableKeepAlives:   false,
    },
    Timeout: 10 * time.Second,
}
```

## 4. 音频缓存

```go
// Redis key: tts:{voice}:{sha256(text)}
// TTL: 24h
func (p *AIPipeline) cachedTTS(ctx context.Context, text, voiceID string) ([]byte, error) {
    key := fmt.Sprintf("tts:%s:%x", voiceID, sha256.Sum256([]byte(text)))
    if cached, err := p.cache.Get(ctx, key); err == nil {
        return cached, nil
    }
    result, err := p.ttsClient.Synthesize(ctx, text, voiceID)
    if err != nil { return nil, err }
    p.cache.Set(ctx, key, result.AudioData, 24*time.Hour)
    return result.AudioData, nil
}
```

缓存内容：比赛开始/结束语、常见感叹（"好球！""差一点！""球进了！！"）

## 5. 延迟监控

```go
// 每个环节打点
type LatencyTracker struct {
    EventArrived  time.Time
    PipelineDone  time.Time
    LLMDone       time.Time
    TTSFirstByte  time.Time
    ClientSent    time.Time
}
```

p50/p95 通过 metrics 暴露，Phase 4 上 Grafana。
