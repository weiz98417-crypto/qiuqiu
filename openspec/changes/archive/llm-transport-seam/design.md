# Design: LLM Transport Seam

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | transport interface 形状：请求构建（信封 + 鉴权头 + 端点）与执行（超时、重试策略、可选熔断）分离；llm 与 router 是两个真实 adapter 证明 seam，不是假设。 |
| 2 | 鉴权约定单一实现：MiMo 平台（baseURL/模型前缀判断）用 api-key 头，其他用 Bearer——判断逻辑从两份手抄收敛为一份；asr / tts 的无条件 api-key 也走同一辅助。 |
| 3 | router 单次调用 = 显式 zero-retry option（不是重新实现）；llm 熔断器挂在 transport 选项上，行为阈值不变。 |
| 4 | 流式（SSE）暂留 llm 侧：transport 首版覆盖非流式信封，流式接口在第二个消费者出现前不抽象（一个 adapter 是假设 seam）。 |
| 5 | 零依赖：内部包，仅 stdlib；不引入 HTTP client 库。 |

## Seam

- seam 位置：transport 包的 Do 接口。llm / router client 变为薄 adapter（业务参数 → 信封）；它们的既有测试成为 seam 的行为等价证明。

## Testing decisions

- 等价证明：llm client_test 与 router 信封 httptest 测试在迁移后原样绿（先例：intent_router_test 的 stubRouterServer 模式）。
- 新增：鉴权分支的表驱动测试（MiMo URL / 非 MiMo URL / 模型前缀）；重试 option 开/关各一条。
- asr / tts 测试不动（只换了共享辅助，行为不变）。

## Further notes

- 落地时评估是否记 ADR（transport 统一 + 流式留白的决定）：满足难逆 + 无上下会意外 + 真实取舍三条件，倾向记一条简短的。
