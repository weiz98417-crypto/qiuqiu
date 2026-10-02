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

> 2026-09 修订（tts-provider-seam）：TTS 从「仅鉴权复用」扩大为收敛进 Provider seam——`internal/tts` 定义 `Synthesizer` 接口（整段 `Synthesize` + `VoiceOpts`，另预留 `SynthesizeStream` 流式位，现役 adapter 返回 ErrNotSupported 不做假流式），Miimo 现役实现与确定性 fake 是它的两个 adapter，供应商知识（URL/model/voice/WAV 组包/熔断器）收敛进 adapter，调用方只见接口。本修订不触碰上列被否决方案「统一信封覆盖 asr/tts」：TTS 的 audio 载荷形状原样（`message.audio.data` 出），收敛的只是调用点与供应商知识。ASR 维持现状——音频上传方向、分片流式转写语义与 seam 的整段合成形状不匹配，等出现第二个 ASR 供应商需求再议。

> 2026-09-30 修订（voice-streaming-delivery）：流式位变现役——MiMo `mimo-v2.5-tts` 基础模型的 `stream:true` SSE（pcm16@24kHz）经 `SynthesizeStreamDetailed` 落地（可选能力接口 `StreamingSynthesizer`，`Synthesizer` 主接口签名不变，测试替身零迁移）；当年拒绝的是「假流式」，真流式正是预留位的本意。回退语义：SSE 断流/超时/非 200/零分片自动降级整段 `Synthesize`（Degraded 分片 REPLACE 契约，调用方无感），调用方取消（打断）不回退不熔断——「剩余句不再合成不再下发」优先于交付完整。同轮在投递层立一条与生态实践的**刻意分歧**：pipecat 的打断语义含「未播文本不入 assistant 上下文」，qiuqiu **不取**——文字气泡整段先行投递、对话上下文以全文记（与 append_turn 语义一致）；音频可被打断，已投递的文本是会话事实。句粒度协议：`voice_audio` 帧增 `sentenceIndex`（>0 才发，整段路径 wire 不变），一句一帧、帧帧完整可播（pcm16 分片服务端聚合成 WAV@24k），投递七态保持话轮级不新增形态。

> 2026-10-01 修订（voice-streaming-delivery 降级）：句粒度投递经韵律盲测（10 对双盲，逐句版 80% 被嫌弃，远超 30% 判据线）裁决**降级回整段合成**——MiMo 每句独立合成的句界不连续无法靠指令完全弥合，整段的全文韵律上下文不可替代。延迟代价（首响实测 3-7s）如实接受。交付物沉淀：投递缝深化（deliveryKey 包/双装配线合一）与猎虫修复全数保留；`tts.StreamingSynthesizer` 流式合成与 `internal/speech` 句聚合器作为已测库保留，未来原生流式供应商（真流式 TTS 而非句批 HTTP）出现时可复活。
