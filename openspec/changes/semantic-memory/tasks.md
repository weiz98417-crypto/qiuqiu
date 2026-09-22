# Tasks: Semantic Memory

- [x] 1.1 internal/embedding 客户端 + 单测（httptest）。
- [x] 1.2 migration 046 + internal/memory/vector_pg.go（Store/Search）。
- [x] 1.3 Queue.WithVectorRecall：Observe 异步嵌写 + Recall 双路合并去重 + 降级矩阵。
- [x] 1.4 main.go 装配（EMBEDDING_* 配置、有库有端点才启用）+ backend/.env.example。
- [x] 1.5 router 迁 structured.Extract + schema 形状锁测试 + structured.CallOptions.ContextMessage。
- [x] 1.6 双词汇统一：以 relationship types.go 映射注释收敛；触及策略内核的重构如实收缩（两域生命周期不同，强并风险大于收益）。
- [x] 1.7 验证：全量 go test + 100 eval 绿；向量路单测（服务在场才跑断言）。

## 触发型留尾（Q3 标准格式）

- 触发：真实部署跑通（EMBEDDING_BASE_URL 配置 + 真实用户数据积累）；动作：向量路真库 E2E 断言（真 embedding 写入→真查询命中），并复核双路配额的实际体感。
- 触发：router key/代理可用；动作：真路由冒烟（迁移后 schema 形状锁已替代离线验证，真网行为一次抽样比对）。

## Sequencing

第二波第 1 个；embedding 通道是 knowledge-rag（候选 2）的复用前提。
