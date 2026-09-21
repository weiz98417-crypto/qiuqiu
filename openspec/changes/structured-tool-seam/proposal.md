# Structured Tool Seam: openaicompat 之上的结构化输出深模块

## Why

结构化 LLM 输出已有两处家酿管道：router 自抄一整套 OpenAI function-call payload/响应解析（router.go:117-155），directordraft 用 prompt-JSON + 首尾大括号截取（extractor.go:44-56）——任何新的结构化消费者都要再抄一遍；trace 里的 ToolCall 是手写注解，CompanionToolSchemas 是纯文档表，schema 与行为必然漂移。同时四处零消费方占位（StreamWithMessages、llm/router 的 setAuthHeaders、AgentBoundaryResponse）留而不用。

## What Changes

- 新增 `internal/structured`：`Extract[T]` 一次强制 function-call 的结构化抽取——schema 由结果类型反射生成（invopop/jsonschema，经 goproxy.cn 引入），「schema 与结果类型漂移」从结构上不存在；传输走 openaicompat，韧性归调用方（ADR-0012 不变）。
- directordraft 迁移为第一个 adapter：extractor 改走 structured.Extract，大括号截取删除，prompt 的输出指令改为指向工具 schema（枚举/角色纪律原文保留）。
- 占位清理：llm 删 StreamChunk/StreamWithMessages/setAuthHeaders（零消费方，需要时从传输层重建）；router 删 setAuthHeaders；companion 删 AgentBoundaryResponse；MaxTokens 默认 80 收敛为具名常量 defaultMaxTokens。
- router 挂待迁标记：其自抄 payload 是 structured 的迁移候选，因会字节级改动调用载荷、须带 boundary 路由 eval 重验，单独立项。

## User Stories

1. As a agent 能力扩展者, I want 一个 Extract[T] 就拿到校验过的结构化结果, so that 新技能（提醒/订阅/知识库）不再抄管道。
2. As a schema 维护者, I want schema 从结果类型反射生成, so that schema 与行为永不漂移。

## Non-goals

- router 本轮不迁（ADR-0009 eval 锁行为，迁移单独立项）。
- 不做多轮 agent 工具循环——seam 形状与 router 一致：单次强制工具调用。
- 不动 asr/tts 载荷（ADR-0012：音频专用格式不并入统一信封）。

## Success Criteria

- structured 包单测锁强制工具选择与实参解码；directordraft 全部测试绿；全量 go test 绿、100 eval 绿。
- 删除测试：删掉 structured 包则 directordraft 退回大括号截取——复杂度（结构化契约）集中一处，是加深。
