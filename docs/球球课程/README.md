# 球球课程 · 内容层

这里是球球项目教学与设计文档的**现役**目录。规则见 [ADR-0004](../adr/0004-content-constitution-evidence-anchors.md)，本目录术语表见 [CONTEXT.md](CONTEXT.md)。

一句话：**每个主张带锚点，每章可核查，代码变了重新生成而不是漂移。**

## 目录结构

```
docs/球球课程/
├── CONTEXT.md          内容层术语表（Claim / Anchor / status 锚 / 事实源章节 / Trace）
├── README.md           本文件
├── chapters/           13 篇事实源章节（自 docs/球球全套资料 靶向迁移）
│   ├── NN-slug.facts.json   机器可读层：主张 + 锚点 + status
│   └── NN-slug.md           人可读层：讲解散文，与 facts.json 同步
└── lessons/            课程讲义：《佩德里进球》trace 贯穿 7 讲
```

## 与旧资料集的关系

- [`docs/球球全套资料/`](../球球全套资料/00-资料包索引与使用说明.md) 已**冻结为 legacy**：其生成规则（禁止代码接地、关键词配额）已在 ADR-0004 废除，其描述的 `cmd/*` 微服务从未存在（真实入口 `backend/cmd/server` 单体）。
- 只有 13 篇讲真实子系统的章节被迁移到这里，其余章节（市场/GTM/增长）留在冻结集里作历史参照。
- 迁移时旧文档中的虚构主张被如实标注为 `concept`，不混入已实现事实。

## 章节清单与真实程度

| 章节 | 主题 | 迁移后 status 概览 |
| --- | --- | --- |
| 20 | 产品设计总览 | 以真实系统形态重写 |
| 23 | 比赛事实与公开比分 | 基本已实现（枚举名漂移已修正） |
| 24 | 直播延迟与用户观察协调 | 已实现，协调器比旧文档更细 |
| 27 | 数字球友对话与表达 | 决策/表达分离是真实架构 |
| 28 | 人格宪法与角色边界 | 人格 = 可审计策略表（policy.go） |
| 29 | 关系阶段与信任成长 | 阶段机已实现 |
| 31 | 主动发言、主动沉默与话痨控制 | 部分实现：冷却真实，预算/许可分类是虚构 |
| 32 | 情绪动力学与 Live2D 表演编排 | 旧引擎是死代码已删（ADR-0005），围绕真实 PresentationPlan 重写 |
| 33 | 沉浸式舞台与 Live2D 交互 | 白名单合同已实现 |
| 33A | Agent 运行时与决策编排 | agent.go 单体真实存在 |
| 47 | 比赛事实账本与冲突调和 | 事件溯源 + 重放已实现 |
| 56 | PostgreSQL 持久化与数据生命周期 | 40 个迁移即演化史（实测 010-012 重号、缺 025） |
| 57 | Evals 评测、黄金集与自动回归 | 四档 tier + CI 门已实现 |

各章逐条 status 见其 `.facts.json` 的 `status_summary` 与章末「主张-锚点表」。

## 课程（lessons/）

工程版课程，主线为一条真实 trace：**佩德里进球**——从事件注入 → 事实账本重放 → 人格策略决策 → 回合调度 → 语言实现（438 字节提示词）→ Live2D 表演 → 评测门。

| 讲 | 主题 | 主锚 |
| --- | --- | --- |
| 0 | 系统全景与 trace 预告 | architecture/archify/ 图源 |
| 1 | 事实层：进球如何成为可撤销的事实 | backend/internal/matchstate/fact_ledger.go |
| 2 | 人格层：谁决定球球想做什么 | backend/internal/relationship/policy.go |
| 3 | 调度层：什么时候说、被抢断怎么办 | backend/internal/conversation/scheduler.go |
| 4 | 表达层：438 字节提示词的分工 | backend/internal/companion/realize.go + backend/prompts/v1.0/system.txt |
| 5 | 表演层：从 ResponsePlan 到 Live2D | client/lib/services/presentation_state.dart |
| 6 | 质量层：怎么证明这套东西没坏 | evals/ + scripts/evals/run.mjs |

每讲模板：trace 位置 → 证据锚 → 代码走读 → eval 联动 → 动手作业（在真仓库改一行、跑 eval 看结果）。

## 如何新增内容

1. 新主张先进 `.facts.json`（带锚点），再进 `.md` 散文；
2. 代码变更后：更新受影响锚点 → 重写漂移的散文（未来由生成器代劳）；
3. 验收：每章证据包 ≥5 锚点、status 表完整、无锚段落 ≤30%（见 ADR-0004）。
