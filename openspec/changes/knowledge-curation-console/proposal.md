# 知识策展台:条目 DB 化与运营编辑 MVP

## Why

知识条目是 repo 资产(backend/knowledge/rules/*.yaml,answer 原文即锚点、source/confidence/effective_at,ADR-0017 第二事实域),运营台编辑一直后置;转会窗复查制度已入 ADR-0017(21 条球员档案要定期复查)但没有工具面。生效窗口/幂等重放是领域逻辑,无现成开源(Argilla 维护模式、Label Studio 审核流是企业版)——自建是正解;UI 蓝本抄 Coze Studio(纯 Apache 2.0,Go+React 与 qiuqiu 同构,可直接读源码)的资源列表交互 + FastGPT 的表单编辑形态。「草稿→审核→发布」流是为大团队设计的,qiuqiu 运营一两人,直接保存+审计即足够。

## What Changes

- **存储迁移**:条目 repo YAML → DB——编辑写 DB,运行时读 DB;repo YAML 降级为 seed(首次导入+新部署,导入幂等)。确定性拼装、answer 即锚点、双路检索(关键词+向量)全部不动。ADR-0017 修订随附。
- **策展台 MVP(console 新页)**:条目列表(检索/生效窗口显示/**「待复查」过滤器**——按 effective_at 到期与转会窗标记)+ 单条编辑(字段表单化:question/answer/source/confidence/effective_at)+ 保存即生效(写路径走 executeOperatorWrite + appendAudit 纪律);到期提醒推送后置。
- **新页随施工带 vitest 组件测试**(导演页 448 行零组件测试的教训不重犯)。
- 运行时生效验证:编辑后 eval 断言知识回答用上新条目(检索双路对新存储)。

## User Stories

1. As a 运营, I want 在运营台编辑知识条目, so that 转会窗复查不用改 YAML 发版。
2. As a 运营, I want 待复查过滤器, so that 到期/转会窗条目不遗漏。

## Non-goals

- 版本 diff / 草稿→发布流(后置,按需)。
- 到期提醒推送(后置)。
- 知识回答生成本身(确定性拼装不动,ADR-0017)。
- 检索算法改动(关键词+向量双路照旧)。

## Success Criteria

- 编辑→运行时生效 e2e(eval 断言新条目被回答引用);
- seed 导入幂等(重跑不重复);
- 审计落账(谁改了什么,operator 写路径纪律);
- vitest 组件测试绿;go 全量 + pr tier 绿。
