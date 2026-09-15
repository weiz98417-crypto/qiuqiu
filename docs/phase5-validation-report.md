# 阶段 5 端到端验证报告

更新日期：2026-07-17

## 1. 结论

阶段 5 的本地演示验收已具备自动化证据。用户会话隔离、事实生命周期、用户端投递去重、服务重启恢复、删除竞态和导播幂等均有对应测试。生产灰度仍受两项已知架构风险约束：WebSocket 投递没有客户端 ACK/持久化重放游标；部分非事务外部副作用仍存在跨崩溃重复执行窗口。

## 2. 覆盖矩阵

| 验收目标 | 自动化证据 | 核心断言 |
| --- | --- | --- |
| 双会话数据隔离 | `scripts/evals/session-isolation-e2e.mjs` | 会话令牌绑定服务端用户身份；伪造 `userId` 被拒绝；用户与导播令牌不能串用；隐私查询忽略他人 `userId` 参数 |
| 事实生命周期 | `tests/evals/fact-lifecycle.spec.mjs` | `provisional` 不公开；确认后公开；撤销后回滚；修订后旧事件不再进入公开视图；再次确认只公开最新修订 |
| 比分、问答、主动播报 | `scripts/evals/runtime-e2e.mjs`、`tests/evals/workflows.spec.mjs` | 导播事件到用户端、追问记忆、比分纠正和 Trace 可追溯 |
| 轮播与投递去重 | `tests/evals/client-delivery-dedupe.spec.mjs` | 同一 `deliveryKey` 不重复展示、播音或执行动作；新事实修订正常投递 |
| 导播重复点击和网络重试 | `tests/evals/operator-control.spec.mjs`、`backend/internal/operatorwrite/postgres_integration_test.go` | 双击只提交一次；相同幂等键可重放；并发、失败和租约过期可恢复 |
| 服务重启恢复 | `scripts/evals/docker-restart-e2e.mjs`、`backend/internal/matchstate/postgres_test.go`、`backend/internal/companion/postgres_integration_test.go` | Docker 后端进程重启后，HTTP 状态、已签发会话令牌、WebSocket 和事实账本恢复；存储重开后来源游标、Trace 和对话保持一致 |
| 删除与异步写入竞态 | `backend/internal/companion/privacy_integration_test.go` | 删除等待在途写事务；随后清除刚提交的数据；删除后的迟到写入返回 `ErrDataDeleted` |
| Outbox 失败重试 | `backend/internal/matchstate/postgres_test.go` | 事件、幂等记录与 Outbox 原子提交；失败投递进入重试并最终发布 |

## 3. 执行入口

日常 PR 验收：

```powershell
node scripts/evals/run.mjs --tier pr
```

发布前验收：

```powershell
go test ./...
go vet ./...
flutter test
node scripts/evals/run.mjs --tier release
```

PostgreSQL 集成测试需设置 `DATABASE_URL`，并至少运行：

```powershell
go test ./internal/matchstate ./internal/companion ./internal/privacy ./internal/relationship ./internal/operatorwrite -run Postgres -count=1
```

本地 Docker 进程级重启验收需先通过环境变量提供导播令牌：

```powershell
node scripts/evals/docker-restart-e2e.mjs
```

## 4. 灰度门槛

本地演示环境可以继续使用 `AUTH_MODE=session`。切换外部生产流量前必须满足：

1. 为用户端投递增加客户端 ACK、持久化游标和断线重放边界。
2. 明确配置、自动化、信号源、人工接管和演示重置等非事务副作用的跨崩溃语义。
3. PR 级、发布级和 PostgreSQL 集成测试全部通过。
4. 灰度期间监控鉴权失败、事实状态转换、幂等冲突、Outbox 重试和公开事实一致性。

## 5. 当前测试结果

- 双会话隔离验收：通过。
- 事实生命周期 Playwright 验收：通过。
- 删除与在途写入 PostgreSQL 竞态验收：通过。
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `flutter test`：25 项通过。
- PostgreSQL 五个核心包集成测试：通过。
- Docker 后端进程重启、会话复用与 WebSocket 重连验收：通过。
- `node scripts/evals/run.mjs --tier pr`：19 项通过，1 项浏览器自动播放场景按环境跳过。
