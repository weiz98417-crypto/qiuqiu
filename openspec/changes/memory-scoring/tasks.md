# Tasks: 记忆打分收编

- [ ] 8.1 三因子收编:adapter 腿打分升级(importance 参与+衰减对齐),两腿同代;evals:排序方向性断言。
- [ ] 8.2 反思触发:重要性累加阈值(定时兜底保留);evals:触发次数不升/无效反思降。
- [ ] 8.3 写回 dry-run:审查 LLM 一步(矛盾/噪声/红线),不过审记审计跳过;evals:矛盾注入被拦。
- [ ] 8.4 Memobase 核对:0.0.36/37/40 能力盘点表(gist/context API 用没用);升级镜像 + 回归;token 对照记录。
- [ ] 8.5 门禁:go 全量 + evals。

## Sequencing

波3。依赖 agent-internals(波2)的 3.4 Queue 拆分先落(RecallFusion/ReflectionEngine 模块化后本 change 改动面才干净)。Memobase 升级(8.4)独立可先行,但 token 对照在 8.1-8.3 落地后测才有意义。
