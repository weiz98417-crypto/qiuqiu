# Tasks: 知识检索加深

- [ ] 5.1 双路融合:关键词+向量并行评分归一化 Top-K,并列消歧显式化;memory 三腿形态移植(接口不变,实现加深)。
- [ ] 5.2 阈值 config 化:cos/confidence 进 config(env 覆盖,默认值=现硬编码)。
- [ ] 5.3 检索面扩展:Answer/Quote 参与向量检索。
- [ ] 5.4 缓存内容键:topicVecs 按内容 hash,Reload 增量重嵌。
- [ ] 5.5 A7 evals(本 change 分摊):检索排序套件(多 topic 交叉/向量优势查询/阈值边界/并列消歧/缓存命中),DB store 模式进 eval。
- [ ] 5.6 门禁:go 全量 + 策展台前后端测试绿。

## Sequencing

波2,独立于 agent-internals/policy-bits(知识域自包含)。是 knowledge-worldinfo(波3 尾)的前置——检索质量先立,预算/互斥参数学才有意义。embedding 依赖本机 Ollama bge-m3(已部署)。
