# Design: Router Trace Durability

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | Migration 044：`ALTER TABLE agent_traces ADD COLUMN IF NOT EXISTS router JSONB`（先例 019/030 的幂等模式）；读写镜像 fact_claim 列的序列化方式。 |
| 2 | 统一 guard：`guardValidateReply(input, candidate, anchors, decision)`——锚源统一为 compactAnchors(全文, reliable, requiredAnchors)，realizer 与 router 两路径共用；router 拒绝写 trace reason（新增常量），不再静默返回空串。 |
| 3 | 锚源收紧是已批准的行为变化（评审 Q2）：路由闲聊回复被拦率可能上升，由新 reason 码在 trace 中可观察。 |
| 4 | reason 码常量表放 companion 包内（字符串常量，不引入枚举序列化复杂度）；27 处裸赋值改为引用常量，不改码值（trace 兼容）。 |
| 5 | RouterTrace 增加拒绝原因字段；ReplyUsed 语义保持「是否采用了路由回复」，拒绝原因独立承载。 |
| 6 | TurnRouter fake 随接口已存在的 seam 注入（WithRouter），测试按用户文本脚本化返回；httptest 桩仅保留一条测 client 信封与鉴权。 |

## Seam

- 持久化 seam：PostgresTraceWriter / scanTrace 的既有读写面——router 列成为 fact_claim 列的镜像，两个 adapter（内存 / Postgres）行为一致。
- guard seam：两条回复路径共用的校验函数——interface 即测试面，表驱动测「拒/放」用例。

## Testing decisions

- trace round-trip：写后读回断言 RouterTrace 完整（含拒绝原因）；Postgres 集成测试沿用 DATABASE_URL self-skip 模式。
- guard 统一：同一组违规样本（禁语、超句数、无锚主张）分别在两路径断言同判——这是「一处定义」的证明。
- 回归：conversation_regression_test 与 intent_router_test 全绿；evals pr 档绿（无 ROUTER key 时路由禁用路径行为不变）。
- 先例：fact_claim 列的持久化测试、realize_fallback_policy 的 trace 断言。

## Further notes

- reason 码值不变意味着历史 trace 仍可读；新增码（router 拒绝）进常量表即可。
