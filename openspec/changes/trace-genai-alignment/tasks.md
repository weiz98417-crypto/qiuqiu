# Tasks: Trace GenAI 语义对齐

- [x] 1.1 常量集中:companion/tracefields.go 单一源(26 个工具名常量,含 boundary.go schema 镜像同源化);全部构造点改引常量;守卫测试 tracefields_test.go 禁止生产代码新增裸字面量(测试文件豁免)。
- [x] 1.2 gen_ai.* 对齐:RouterTrace.Model 新增(↔ gen_ai.request.model,router.Client 增 model 字段与 Model() 访问器,TurnRouter 走可选接口断言不动 seam);语音段锚点保留领域名;映射表落 docs/design/trace-genai-alignment.md(对齐清单/保留清单/理由/消费面结论/mcp 交叉约定)。
- [x] 1.3 消费面同步:核对结论=零变更——语音锚点保留领域名,latencyStages/toolCalls 键未动,Grafana 六面板与 console TurnReplay 消费的键全部不变;router.model 为新增可选字段,现有渲染不感知。
- [x] 1.4 eval 断言+门禁:守卫测试即字段名断言(字面量只许出现在 tracefields.go);go build 全仓 + companion/router/cmd/server 包测试全绿。

## Sequencing

波A。**必须在 voice-streaming-delivery 之前落地**——①的新锚点 tts_first_audio 直接用新字段体系(voice_stages.go 追加常量),避免二次改名;user-voice-affect 的 user_affect 键与 mcp-registry-serve 的 gen_ai.tool.* 同理依赖本 change。
