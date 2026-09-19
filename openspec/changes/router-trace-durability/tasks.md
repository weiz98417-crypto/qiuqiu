# Tasks: Router Trace Durability

- [x] 1.1 Migration 044（agent_traces.router JSONB）。
- [x] 1.2 PostgresTraceWriter / scanTrace 读写 RouterTrace；round-trip 测试。
- [x] 1.3 reason 码常量表；27 处赋值改引用（码值不变）。
- [x] 1.4 guardValidateReply 统一两路径（锚源含 requiredAnchors）；RouterTrace 拒绝原因字段。
- [x] 1.5 TurnRouter fake；agent 级路由测试迁移；保留一条 httptest 信封测试。
- [x] 1.6 验证：go test 全绿 + postgres 集成（有 DATABASE_URL 时）+ pr 档 evals 绿。

## Sequencing

第五个执行：契约锁之后、agent 大提取之前——guard 统一先行，agent-engine-extraction 搬移的就是统一后的代码。
