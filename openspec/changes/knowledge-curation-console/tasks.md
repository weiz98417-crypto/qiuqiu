# Tasks: 知识策展台 MVP

- [x] 7.1 存储迁移:knowledge Store 增 DB 实现(条目表:question/answer/source/confidence/effective_at + 审计元数据);repo YAML seed 导入器(幂等,重跑不重复);运行时读切换;双路检索(关键词+向量 embedding)对新存储回归。
- [x] 7.2 运营 API:条目列表(含待复查过滤参数)/单条读/改——save 即生效;走 executeOperatorWrite + appendAudit;schema 校验与 ADR-0017 字段一致。
- [x] 7.3 console 策展页:列表(检索/生效窗口/待复查过滤)+ 编辑表单(Coze 资源列表 + FastGPT 表单蓝本);vitest 组件测试随页(列表/表单/过滤器)。
- [ ] 7.4 ADR-0017 修订 + 门禁:存储 DB 化+seed 降级如实记录;编辑→运行时生效 e2e;go 全量 + pr tier + vitest。
  - ADR-0017 修订注已落(2026-09-30,storage DB 化 + seed 降级);vitest 全绿、tsc/vite build 绿、go knowledge + cmd/server 包全绿;go 全量/pr tier/eval e2e 留待全量门禁跑(施工带纪律:只跑触及包)。

## Implementation Notes

- 字段口径:repo 条目的事实单元实际是 `topics`(匹配关键词,ADR-0017 决定 3)而非独立 question 字段——条目表照 Entry 结构落 `id/topics/answer/source/confidence/effective_at`(+判罚附句的 `triggers/quote`)+ 审计元数据 `created_by/created_at/updated_at`;运营台表单的「匹配关键词(topics)」即提案里的 question 侧。
- 转会窗标记:YAML 无显式转会窗字段,「待复查」按 ADR-0017 复查制度推导——生效窗口早于最近一次窗闭(每年 7-01/1-01 UTC)即标记。

## Sequencing

波D。独立于语音/形象侧;与 mcp-registry-serve 无文件交叉(四只读工具读赛程与比赛事实,不读知识条目)。

## CEO 审查外部声音增量(2026-09-30)

- **DB 孤本风险**:条目迁 DB 后 repo YAML 必然发散为陈旧副本(编辑不回写),DB 无版本/无导出——git 评审安全网对知识内容失效。留尾下轮:**条目导出(回 YAML 或下载)**;备份纪律已完成一半(e108c26:deploy/README 点名 knowledge_entries 在整库 dump 内——上表即备份),导出仍缺。新条目入口收窄到策展台(repo YAML 只读化注记进 README 防新人踩坑)。
