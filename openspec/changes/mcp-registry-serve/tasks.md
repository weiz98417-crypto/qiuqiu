# Tasks: MCP 工具层(波1)

- [x] 5.1 ToolRegistry:interface + 注册表 + 内置 handler 入池迁移;CompanionToolSchemas 淘汰(schema 从注册表生成,23 名称保持兼容);trace 字段 `gen_ai.tool.*`(trace-genai-alignment 体系);注册表↔trace 一致性断言用例。
- [x] 5.2 反向只读 server:go-sdk 依赖锁版本;`/mcp` 路由挂 ServeMux;JWT 只读 scope 中间件(匿名拒/写调用拒/越 scope 拒);四只读工具直调 matchstate/schedule 既有读函数;MCP inspector 实测往返。
- [x] 5.3 红线与治理:白名单 config 结构(波2 消费)、2s 预算与静默降级语义文档化;宪法负例测试(MCP 路径对事实账本零写入)。
- [ ] 5.4 ADR-0022 + 门禁:ADR 落库(分层与红线);go 全量 + pr tier。

## Implementation notes(5.2/5.3,2026-09-30)

- go-sdk:`github.com/modelcontextprotocol/go-sdk v1.8.0`(go.mod 直依赖;默认 GOPROXY 不通,经 goproxy.cn 拉取)。API 形态:`mcp.NewServer` + 原生 `Server.AddTool`(v1.8.0 要求显式 `InputSchema`——由 companion 注册表描述的 `Input` map 生成 JSON Schema,保持单一源)+ `NewStreamableHTTPHandler`。
- 包:`backend/internal/mcpserve`(server.go 四工具+挂载、auth.go Principal/ReadScope 缝、doc.go 治理文档);装配在 `backend/cmd/server/mcp_api.go`(`mcpIdentityResolver`:consoleauth JWT + operatorauth 个人令牌,刻意不走 legacyOperatorClaims 旁路),main.go 一行 `mountMCPServer(...)` 挂 `/mcp`。
- 鉴权:运营台 ADR-0010 同体系;`operator:trace:read`(director/auditor 均有)可读;匿名/无效凭证一律 401(legacy 共享 APP_TOKEN 开发旁路对 /mcp 不生效);缺 scope 403。
- 工具口径:`PublicSnapshot`/`PublicEvents` + `TodayFixtures`,active 过滤与 limit 钳制(≤50,默认 8)对齐 companion agent 既有读路径。
- 测试:mcpserve 18 例(in-memory client↔server 往返 tools/list+四工具各一例、limit 钳制、未配赛程源工具错误、HTTP 401/401/403/放行、注册表一致性、宪法负例三闸:MutatesMatchFacts==false/源码无写路径标识/未登记 `match.confirm_fact` 协议层报错);cmd/server 9 例(真实 JWT 错签/过期 401、临时密码无 scope 403、auditor JWT/个人令牌放行、legacy 旁路死亡、装配烟测)。
- MCP inspector 实测:本机 node v24.19.0 可用,`npx @modelcontextprotocol/inspector --cli --transport http` 对运行中 server(端口 18099)完成真实往返——tools/list 恰四工具、`match.read_snapshot` 返回 demo 比赛快照(text+structuredContent)、匿名连接被拒、`match.confirm_fact` 探测返回 `tool_not_found`。
- 已知并行施工注意:cmd/server 全量测试在另代理编辑 response_delivery.go 中途会出现编译错,与本 change 无关。

## Sequencing

波C。ToolRegistry 触及 companion 包(boundary.go/intent_registry.go)与 voice-streaming-delivery(投递缝,conversation/cmd/server)文件不交叉,可并行;依赖 trace-genai-alignment(工具 trace 字段定名)。波2 正向消费(白名单 server 选型+handler 接 MCP 实现)留尾,治理已由本 change 立法。
