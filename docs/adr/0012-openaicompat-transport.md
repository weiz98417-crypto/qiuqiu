# 0012 · OpenAI 兼容传输层统一（openaicompat）

## 背景

仓库内有五处手搓的 OpenAI 兼容 HTTP 调用（llm×2 调用点、router、asr、tts），MiMo 平台鉴权启发式（api-key vs Bearer 分支）在 llm 与 router 各手抄一份并已注明互为镜像；韧性策略分叉（llm 有熔断器，router 无）。每接入一个新模型平台都要把这组决定重抄一遍。

## 决定

1. **单一传输层** `internal/openaicompat`（零依赖）：鉴权分支（`UsesAPIKeyAuth`/`SetAuthHeaders`）与 POST 执行（`Post`：鉴权 + Content-Type + 状态检查 + 限量读体）一处定义。llm 与 router 是它的两个真实 adapter——五个 adapter 证明这是真 seam。
2. **韧性策略归调用方**：传输层默认零重试。llm 在自身调用序列上保留熔断器；router 的单次调用（ADR-0009）即「不开重试」的原样表达，不新增机制。
3. **asr/tts 只复用鉴权辅助**：其载荷是音频专用格式（`input_audio` 入、`message.audio.data` 出），不并入统一 JSON 信封。其模型恒为 `mimo-` 前缀，启发式分支下鉴权行为与原无条件 api-key 等价。
4. **流式（SSE）暂留 llm**：只有它一个消费者。第二个消费者出现前不抽象——一个 adapter 是假设 seam，两个才是真的。

## 被否决的替代方案

- **统一信封覆盖 asr/tts**：协议同端点但载荷不同形，强行合并需要载荷 side-band，复杂度高于收益。
- **外部 HTTP 客户端库**：零依赖纪律（ADR-0010 同款）不破。
- **传输层内置重试/熔断**：会把 llm 的熔断语义与 router 的单次调用语义错配成一层配置；策略属于调用方。

## 后果

- 换 OpenAI 兼容平台只改配置；平台鉴权约定变更只改 `openaicompat` 一处。
- 新增表驱动鉴权分支测试（transport_test.go），llm/router/asr/tts 既有测试作为行为等价回归网。

> 2026-09 修订（structured-tool-seam）：llm 的流式半成品 StreamWithMessages 因长期零消费方删除，"SSE 暂留 llm"的假设随之撤销——需要流式时从传输层重建；invopop/jsonschema 之上的 structured.Extract 成为 openaicompat 的第三个消费者。
