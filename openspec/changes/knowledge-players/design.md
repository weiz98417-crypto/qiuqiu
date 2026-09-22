# Design: Knowledge Players

## 扒料→策展管线

AnySearch `search`（交叉确认 ≥2 源：官网/维基/百科）→ `extract` 整页 → 人工策展为条目（answer 写成球球口吻的口语，来源字段记确认过的出处）→ 落 `backend/knowledge/players/*.yaml` → git 评审即发布。策展纪律与 ADR-0017 一致：条目原文即答案锚，绝不生成。

## 条目形状

球队条目 topics：队名全称 + 常用别名（皇马/皇家马德里并排）；answer 覆盖成立/主场/荣誉/风格（慢变）。球员条目 topics：中文全名 + 常用中文简称 + 外文名；answer 覆盖国籍/位置/现属（标截至窗口）/生涯里程碑/风格。现属俱乐部等快变字段的 answer 内显式带"截至 2026 年夏窗"措辞，让条目自带保质期标签。

## 转会窗复查制度（ADR-0017 后果补充）

每年 7 月与 1 月两个转会窗关闭后，对 players 目录的快变字段做一次集中复查（AnySearch 逐条核对），变更走 git 提交。这是人工制度不是代码——记入 ADR-0017 后果与本 change 验收说明；未来 data-provider-lite-bridge 落地后可由源数据自动替换。

## 触发型留尾（Q3 标准格式）

- 触发：data-provider-lite-bridge 落地；动作：以源数据替换球队/球员策展条目（本 change 的策展条目随之退役或降级为兜底）。
- 触发：AnySearch 服务不可用；动作：扒料暂停，已有条目不受影响（策展发生在开发期，不在运行时）。

## 删除测试

删掉 players 目录：knowledge 域退回规则/赛制条目，无悬空依赖。
