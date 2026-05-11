# Resilience 技术设计

## 1. WebSocket 断连重连

```go
func ReconnectLoop(addr string, maxRetries int) (*websocket.Conn, error) {
    backoff := 1 * time.Second
    for i := 0; i < maxRetries; i++ {
        conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
        if err == nil {
            return conn, nil
        }
        log.Printf("reconnect %d/%d failed: %v (retry in %v)", i+1, maxRetries, err, backoff)
        time.Sleep(backoff)
        backoff = min(backoff*2, 8*time.Second)
    }
    return nil, fmt.Errorf("max retries exceeded")
}
```

## 2. API 降级矩阵

| 故障 | 检测 | 切换 |
|------|------|------|
| LLM 超时 >2s | context deadline | Ollama 本地 |
| LLM 错误率 >50% | 滑动窗口 | Ollama 本地 |
| Ollama 不可用 | 连接 refused | 兜底模板 |
| TTS 超时 >5s | context deadline | 文字气泡 |
| TTS 全静音 | RMS < 阈值 | 重试 1 次→文字气泡 |
| TTS 超短/超长 | <0.3s or >15s | 重试 1 次→文字气泡 |

## 3. 数据源异常

```
数据源断连检测:
  15s 无新事件 → token仍然有效 → Live2D idle + 提示"信号好像断了..."
  30s → token可能过期 → 重新认证
  60s+ → 确认断连 → "连接不太稳定，等网络好了叫我～"
  重连成功 → "好了好了！信号回来了！" + 恢复陪看
```

## 4. 句式池扩充

每种事件类型 10+ 种变体，LLM 生成时参考但不直接使用：

**进球**：感叹式/评价式/比分式/转折式/遗憾式/球员式/时间式/连进式/乌龙式/点球式

**射门**：可惜式/期待式/夸赞式/吐槽式/门将式/擦柱式/高出式/偏出式/补射式/定位球式

**黄牌**：客观式/吐槽式/球员式/疑问式/累积式

**红牌**：震惊式/可惜式/转折式/疑问式

## 5. 兜底模板覆盖

```go
var fallbackTemplates = map[string][]string{
    "goal":     {"球进了！！", "{player}破门！！", "漂亮！这球太关键了！"},
    "shot":     {"好球！", "差一点！", "这脚有威胁！"},
    "yellow_card": {"吃牌了...", "{player}领到黄牌"},
    "red_card": {"红牌！！", "{player}被罚下！这下麻烦了"},
    "match_start": {"比赛开始了！一起看吧～"},
    "match_end":   {"比赛结束！"},
    // ... all 12 event types
}
```
