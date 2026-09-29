# Trace 字段 GenAI 语义映射(trace-genai-alignment)

日期:2026-09-30 · 变更:openspec/changes/trace-genai-alignment

## 背景

qiuqiu 的自建 trace(五段语音延迟分解、事后 attach 原子合并落 PG、traceID 贯穿)是领域资产,不重建为 OTel span 树。本变更把「未来向任何 OTLP 后端(Phoenix/Langfuse/Tempo)双写不用重映射字段」的对齐成本降到最低:命名约定收敛 + 常量单一源 + 本映射文档。**只做命名对齐,不引入 OTel SDK,不改存储与 attach 体系。**

生态依据:OTel GenAI 语义约定(已迁独立仓库 open-telemetry/semantic-conventions-genai,未 stable)是 Langfuse/Phoenix/OpenLLMetry 三端共同收敛方向。

## 对齐清单(采用 gen_ai 语义)

| qiuqiu trace 字段 | OTel GenAI 对应 | 说明 |
|---|---|---|
| `trace.toolCalls[].name` | `gen_ai.tool.name` | 值本身是语义工具名(如 `match.read_snapshot`),常量单一源于 `companion/tracefields.go`(守卫测试 `tracefields_test.go` 禁止生产代码新增裸字面量) |
| `trace.toolCalls[].args` | `gen_ai.tool.args`(JSON) | 参数 map 原样 |
| `trace.router.model` | `gen_ai.request.model` | intent_router.go 从 router.Client.Model() 填充;分类调用属 LLM 调用类属性 |
| `trace.router.intent/confidence` | 生态无直接对应 | ADR-0009 分类语义,保留领域名 |
| `trace.latencyMs` | `gen_ai.client.operation.duration` 的文档形 | 保留毫秒整数字段,映射表记录换算 |

## 保留清单(不映射,理由)

| 字段/锚点 | 理由 |
|---|---|
| 语音延迟锚点 `speech_received` / `asr_final` / `turn_decided` / `tts_synthesized` / `audio_delivered`(companion/voice_stages.go 单一源) | OTel GenAI 语音段约定未 stable(OpenInference 的 realtime voice-agent tracing 刚并入 PR #3173,provider 中立约定在草案);不为对齐而对齐,等 stable 再评估。**anchor 集合将由 voice-streaming-delivery 扩至六个(新增 `tts_first_audio`)**,直接沿用本体系 |
| `trace.voice.*`(asrStatus/asrText/asrProvider/ttsStatus 等) | 领域观测语义,生态无对应 |
| `trace.deliveryState`/playback 三态(source: client/client_late/server_inferred) | 领域资产(delivery-outcome-uplink),生态无对应 |
| `latencyStages` JSON 键名 | Grafana 六面板与 console TurnReplay 的消费面;键未变,面板零变更(本变更已核对:因锚点保留领域名,面板查询无需改动) |

## 消费面同步结论

- **Grafana 面板**:零变更——本变更未改任何面板消费的键(latencyStages/stage 锚点/toolCalls 均保留原名)。
- **console TurnReplay**:零变更——同理。`router.model` 为新增可选字段,console 现有渲染不感知、不破坏。
- 后续新 trace 字段纪律:**新字段的字符串名必须落常量**(工具名→tracefields.go;锚点→voice_stages.go;若两处都不合适则新建常量文件),映射表随字段新增更新本节。

## mcp-registry-serve 交叉约定

工具调用 trace 字段按上表 gen_ai.tool.* 语义定名;ToolRegistry(波C)落地时,注册表的工具名直接复用 tracefields.go 常量——注册表、schema 镜像、trace 三处同源。
