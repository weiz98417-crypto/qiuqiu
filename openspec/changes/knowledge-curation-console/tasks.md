# Tasks: 知识策展台 MVP

- [ ] 7.1 存储迁移:knowledge Store 增 DB 实现(条目表:question/answer/source/confidence/effective_at + 审计元数据);repo YAML seed 导入器(幂等,重跑不重复);运行时读切换;双路检索(关键词+向量 embedding)对新存储回归。
- [ ] 7.2 运营 API:条目列表(含待复查过滤参数)/单条读/改——save 即生效;走 executeOperatorWrite + appendAudit;schema 校验与 ADR-0017 字段一致。
- [ ] 7.3 console 策展页:列表(检索/生效窗口/待复查过滤)+ 编辑表单(Coze 资源列表 + FastGPT 表单蓝本);vitest 组件测试随页(列表/表单/过滤器)。
- [ ] 7.4 ADR-0017 修订 + 门禁:存储 DB 化+seed 降级如实记录;编辑→运行时生效 e2e;go 全量 + pr tier + vitest。

## Sequencing

波D。独立于语音/形象侧;与 mcp-registry-serve 无文件交叉(四只读工具读赛程与比赛事实,不读知识条目)。
