# Performance Optimization — 性能优化

## 目标

将端到端延迟从当前 P95 ~4.4s 压缩到 < 2s，确保用户感知延迟在可接受范围。

## 范围

| 模块 | 内容 | 当前 | 目标 |
|------|------|------|------|
| Prompt 压缩 | System Prompt 从 ~200 token 压到 ~150 | 1.0s | <800ms |
| 流式 TTS | 句子分割→边生成边合成→边下推 | 2.4s | <800ms 首包 |
| HTTP 长连接 | Keep-Alive，避免重复 TCP 握手 | — | -200ms |
| 音频缓存命中 | 高频内容预生成缓存 | 每次合成 | 0ms(命中) |
| 端到端总延迟 | | P95 4.4s | P95 < 2s |

## 不在范围

- GPU 自部署 LLM（已有 Ollama 备选方案）
- CDN 加速（Phase 4）
- 客户端预加载（Phase 4）

## 依赖

- `ai-pipeline` — LLM + TTS 管道
- `watch-integration` — WebSocket 下发

## 里程碑

- [ ] 流式 TTS 首包 < 800ms
- [ ] LLM 延迟 P95 < 800ms
- [ ] 端到端 P95 < 2s
- [ ] 缓存命中时延迟 < 500ms
