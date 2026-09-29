# Tasks: Trace GenAI 语义对齐

- [ ] 1.1 常量集中:指定单一 trace 字段包,收拢 companion/trace.go、companion/voice_stages.go、observation 各构造点的字段名常量;全部 WriteTrace 点改引常量;守卫测试禁止新增裸字符串字段名。
- [ ] 1.2 gen_ai.* 改名:LLM 调用类属性按约定(model/system/token/finish_reason);语音段锚点保留领域名;映射表(对齐清单/保留清单/理由)落 docs/design/trace-genai-alignment.md。
- [ ] 1.3 消费面同步:Grafana 六面板查询+provisioning 改字段、golden 重锁;console TurnReplay 字段同步;同一提交内改完。
- [ ] 1.4 eval 断言+门禁:trace payload 字段名断言用例;go 全量+pr tier。

## Sequencing

波A。**必须在 voice-streaming-delivery 之前落地**——①的新锚点 tts_first_audio 直接用新字段体系,避免二次改名;user-voice-affect 的 user_affect 键与 mcp-registry-serve 的 gen_ai.tool.* 同理依赖本 change。
