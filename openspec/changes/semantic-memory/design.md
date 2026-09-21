# Design: Semantic Memory

## 接缝位置（F1 实证）

`memory.Queue` 是前台组合根（WithReflections/WithThreads/WithPortraitOverlays 已有先例）：新增 `WithVectorRecall(store VectorMomentStore, embedder Embedder)`。Recall = adapter.Recall（Memobase 拉取+contains 打分，原样）∥ vector 路（pgvector 余弦 top-k），两路各取 limit/2 配额、按 content 去重、合并截断。Observe = 原入队逻辑 + 异步（goroutine，一次重试）嵌 Moment 落 pgvector。

## 组件

- `internal/embedding`：`Client.Embed(ctx, text) ([]float32, error)`，POST {base}/embeddings {model, input}，超时与重试策略归调用方（ADR-0012 同哲学）。配置：`EMBEDDING_BASE_URL`（默认 http://localhost:11434/v1）、`EMBEDDING_MODEL`（默认 bge-m3）。
- `internal/memory/vector_pg.go`：`OpenPostgresVectorStore(ctx, dbURL)`；migration 046 建 `CREATE EXTENSION IF NOT EXISTS vector` + 表；检索 SQL `ORDER BY embedding <=> $1`（余弦距离）。
- 双路合并放 `Queue.Recall`，去重键 = content。

## 降级矩阵（Q12）

| 故障 | 行为 |
|---|---|
| 查询侧 Ollama 超时/不可达（200ms） | 向量路弃权，contains 单路（=现状） |
| 写入侧嵌失败 | 重试一次，仍败则放弃（contains 路已覆盖该 Moment） |
| pgvector 表不存在/查询错 | 向量路弃权，记日志 |

## router 迁移（留尾 3.6）

`router.Route` 改走 `structured.Extract[Result]`：CallOptions 增可选 `ContextMessage`（路由的第三条消息）；Result 的 intent 字段用 `jsonschema:"enum=..."` tag 保持枚举同序同值；单测断言反射 schema 的 properties/enum/required 与迁移前手写 schema 逐项一致；MaxTokens 200/温度 0.1/thinking disabled 不变。evals 走 scripted router 不受影响；真路由行为由 schema 形状锁保真。

## 删除测试

删掉 vector 路与 embedding 包：召回退回 contains 单路（=能力波 #2 现状），router 退回家酿 payload——均为可退安全态，无悬空依赖。
