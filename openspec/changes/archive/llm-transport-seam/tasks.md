# Tasks: LLM Transport Seam

- [x] 1.1 内部 transport 包：信封构建 + 鉴权单一实现 + 超时/重试/熔断 options。
- [x] 1.2 llm client 迁移为 adapter（熔断语义不变）。
- [x] 1.3 router client 迁移为 adapter（zero-retry option，ADR-0009 原样）。
- [x] 1.4 asr / tts 复用鉴权/HTTP 辅助。
- [x] 1.5 鉴权分支表驱动测试；既有 llm/router 测试等价绿。
- [x] 1.6 评估并撰写简短 ADR（transport 统一、流式留白）。
- [x] 1.7 验证：go test 全绿 + pr 档 evals 绿。

## Sequencing

最后一个执行：全部结构性变更收尾时，搬移面已最小。
