# Tasks: Memory Recall Fusion

- [ ] 1.1 PostgresVectorStore.SearchByContent（ILIKE+转义+超时弃权）；MemoryVectorStore 同名实现供单测。
- [ ] 1.2 Recall 三路合并（精确腿从向量路配额内置顶替换；去重；弃权语义）。
- [ ] 1.3 recency 衰减重排（τ=14d 进 config，0=关）。
- [ ] 1.4 单测：实体兜底命中、衰减顺序、去重、弃权=现状；vector_dual_test 扩展。
- [ ] 1.5 验证：全量 go test 绿。

## Sequencing

架构评审 D 项（~1-1.5 人日）。G 的 C 域评测（跨场命中×6、时序推理×4）依赖本项落地后才能写全。
