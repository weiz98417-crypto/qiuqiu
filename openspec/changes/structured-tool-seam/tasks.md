# Tasks: Structured Tool Seam

- [x] 3.1 internal/structured：Extract[T] + CallOptions + 工具调用响应解析。
- [x] 3.2 directordraft 迁移（extractor 走 structured.Extract，大括号截取删除）+ main.go 接线。
- [x] 3.3 占位清理：llm 流式半成品与两处 setAuthHeaders、AgentBoundaryResponse、defaultMaxTokens 具名。
- [x] 3.4 structured 包单测（强制工具选择/schema 反射/无工具调用报错/nil client）。
- [x] 3.5 验证：全量 go test 绿 + 100 eval 绿。
- [ ] 3.6 （待迁，单独立项）router 迁入 structured：payload 字节级变化需 boundary 路由 eval 全量重验。

## Sequencing

能力波第 3 个。proactive-scheduler 的排程解析（④旗舰）将把它当第二个生产 adapter。
