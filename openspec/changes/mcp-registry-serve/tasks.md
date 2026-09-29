# Tasks: MCP 工具层(波1)

- [ ] 5.1 ToolRegistry:interface + 注册表 + 内置 handler 入池迁移;CompanionToolSchemas 淘汰(schema 从注册表生成,23 名称保持兼容);trace 字段 `gen_ai.tool.*`(trace-genai-alignment 体系);注册表↔trace 一致性断言用例。
- [ ] 5.2 反向只读 server:go-sdk 依赖锁版本;`/mcp` 路由挂 ServeMux;JWT 只读 scope 中间件(匿名拒/写调用拒/越 scope 拒);四只读工具直调 matchstate/schedule 既有读函数;MCP inspector 实测往返。
- [ ] 5.3 红线与治理:白名单 config 结构(波2 消费)、2s 预算与静默降级语义文档化;宪法负例测试(MCP 路径对事实账本零写入)。
- [ ] 5.4 ADR-0022 + 门禁:ADR 落库(分层与红线);go 全量 + pr tier。

## Sequencing

波C。ToolRegistry 触及 companion 包(boundary.go/intent_registry.go)与 voice-streaming-delivery(投递缝,conversation/cmd/server)文件不交叉,可并行;依赖 trace-genai-alignment(工具 trace 字段定名)。波2 正向消费(白名单 server 选型+handler 接 MCP 实现)留尾,治理已由本 change 立法。
