# Design: Operations Live Stream

## observingLedger（observer 装饰器）

```go
type LedgerObserver func(event interaction.Event)

type observingLedger struct {
    inner interaction.Ledger
    obs   LedgerObserver
}
```

- 套在 `interaction.Ledger` 接口外（内存与 PG 两个 adapter 不动——缝在接口不在实现）；`Append` 成功后异步投递（带缓冲 channel，饱和即丢+计数），**永不阻塞、永不反伤落库**。
- 广播侧挂 hub 的 ops 主题：事件裁剪成 `{kind, matchId, userId, traceId?, latencyMs?}`——裁剪发生在序列化前，正文字段在结构体层面不存在，而非靠约定不填。
- 隐私断言测试：对含 Phrase/Input 类字段的事件做 round-trip，断言广播载荷无该键。

## /ws/ops 端点

- 复用既有 `/ws/match/` 的 Claims 提升链（JWT/个人令牌/subprotocol），提升完成后查 `TraceRead` scope，无则关连接（与 HTTP 403 同语义）。
- 单一全局主题：连接即订阅全量裁剪事件；无历史回放（要看历史走回放页/面板）。
- 心跳/清理沿用 hub 既有机制。

## 直播监听页

- 左栏：事件流（虚拟滚动，量级按 kind 过滤后单机可控）+ 过滤器（kind/user）；右栏：选中事件的 trace 关联视图（复用波 1 TurnReplay 数据面或跳转 CitationAudit）。
- 双栏交互参照 Chatwoot inbox（左列表右详情），但不引入其任何代码。

## 路由收敛（E）

- `console_api.go` 字符串 switch → `ServeMux` pattern 注册表：每端点一行 `mux.Handle("GET /api/console/threads/{id}", withScope(ScopeTraceRead, threads.Get))`；路径参数经 `r.PathValue` 取。
- `match_operator_api.go` 同法迁移（前缀 `/api/matches/{matchId}/...`）。
- 装饰器：`withScope(scope string)` 包 `authz.authorize`；`withAudit(intent string)` 包既有审计三段式（3s ctx + AppendAudit + 失败仅 log）。原 ~20 处 authorize 与 3 处审计手抄全部收敛为声明。
- **行为不变纪律**：错误状态码/响应体文本原样保留（含裸 http.Error 的历史文本）；ServeMux 的 404/405 与手写 switch 的差异逐一比对（手写 switch 对未知路径现返回什么，迁移后必须一致；golden 已锁 console 读端点，补一轮未知路径断言）。

## 删除测试

- 删 ops 流：observingLedger 摘掉即回原 ledger，账本无损——旁路成立。
- 删装饰器：authorize 回散到各 handler——复杂度回散证明收敛价值。
