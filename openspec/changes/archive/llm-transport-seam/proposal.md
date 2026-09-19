# LLM Transport Seam: 一个 OpenAI 兼容 transport module

## Why

四个模块各自手写了 OpenAI 兼容的 /chat/completions 调用：llm（带熔断器）、router（无熔断，单次调用是 ADR-0009 决定）、asr（音频载荷）、tts（音频载荷）。实测 llm 与 router 的调用逐字节同形——同端点、同 JSON 信封超集、同一套 api-key vs Bearer 鉴权分支（后者是手抄两份，注释自己承认「mirrors the realizer client」）。以后每接一个新模型平台，这组决定（鉴权、信封、韧性）就要重抄一遍；韧性策略已在分叉。

## What Changes

- 新建内部 transport module（零依赖纪律不破）：端点、鉴权约定（MiMo api-key / 其他 Bearer 的单一实现）、JSON 信封、超时与重试策略作为参数。
- llm 与 router 完全接入：同形信封是两个真实 adapter，seam 成立。llm 的熔断器与流式（SSE）作为 transport 的选项/扩展保留行为不变；router 的单次调用策略 = 不开重试的 option（ADR-0009 语义原样）。
- asr / tts 只复用鉴权与 HTTP 辅助（载荷是音频专用格式，不强行同信封）。
- 手抄的 setAuthHeaders 两份合一。

## User Stories

1. As a 模型平台迁移者, I want 换 OpenAI 兼容平台只改配置, so that 不需要在四个 client 里各改一遍鉴权。
2. As a router 维护者, I want 单次调用是 transport 的一个 option, so that ADR-0009 的策略声明式保留。
3. As a llm 维护者, I want 熔断策略可配置, so that 新消费者按需开关。
4. As a 未来 LLM 消费者（如主动文案）, I want 白得一个带鉴权/韧性/信封的 transport, so that 接入只写业务载荷。
5. As a 鉴权规则维护者, I want api-key vs Bearer 只有一处实现, so that 平台约定变更不会漏改一份。
6. As a 零依赖纪律的守护者, I want transport 是内部包, so that 不引入任何外部依赖。

## Non-goals

- 不改任何调用方的业务语义（回复生成、路由判定、识别、合成行为不变）。
- 不合并 asr / tts 的载荷格式进统一信封（协议同端点但不同形，诚实分开）。
- 不做重试风暴防护以外的可观测性新增（trace 侧由 router-trace-durability 覆盖）。
