---
id: 29-relationship-stages
title: 关系阶段与信任成长
source_chapter: docs/球球全套资料/4.产品设计/29-关系阶段与信任成长设计.md
status_summary: { implemented: 10, partial: 2, planned: 0, concept: 1 }
---

# 关系阶段与信任成长

本章管「球球和用户熟到什么程度、熟了多说什么」。真实程度：一个四阶段状态机已实现并按行为证据推进，但它与旧文档的设计方向相反——旧文档坚持阶段只能由用户显式设置触发，现实中阶段由用户线索计数自动升级；旧文档的许可制度与控制面大部分未实现。

## 系统实际怎么工作

**四阶段状态机。** 阶段常量为 first_meeting → familiar → watch_buddy → old_ballmate（implemented-at backend/internal/relationship/types.go:100-105）。每次信号处理后 `advanceStage` 检查升级条件（implemented-at backend/internal/relationship/policy.go:302-320）：

- familiar：共同观看≥2 场 + 至少 2 类证据（implemented-at backend/internal/relationship/policy.go:310-312）；
- watch_buddy：≥3 场 + 至少 3 类证据（implemented-at backend/internal/relationship/policy.go:313-315）；
- old_ballmate：≥6 场 + 召回过共同瞬间 + （持续过分歧或完成过修复）（implemented-at backend/internal/relationship/policy.go:316-319）。

**证据是线索计数器。** 阶段门槛读的是 9 个分类计数：SharedMatches、StablePreferences、ContinuedThreads、AcceptedJudgments、AcceptedInitiatives、AllowedBanterScopes、SharedMomentsRecalled、ContinuedDisagreements、CompletedRepairs（implemented-at backend/internal/relationship/types.go:154-165）。`applyUserCues` 按用户话术递增：「我喜欢/我支持」→ 稳定偏好，「接着上次」→ 续线，「判断挺准」→ 接受判断，「毒奶」→ 调侃许可，「想起上次那场」→ 共同瞬间召回（implemented-at backend/internal/relationship/policy.go:237-300, 189-218）。没有旧文档明确禁止、也明确不建的单一「亲密度分数」。

**修复挡住升级。** RepairState.Active 时 advanceStage 直接返回（implemented-at backend/internal/relationship/policy.go:307-309）；修复完成需连续 3 个正常跟回合并计入 CompletedRepairs（implemented-at backend/internal/relationship/policy.go:139-148）。这对应旧文档状态机里的 R_修复中；旧稿的三个附加状态（P 暂停 / R 修复中 / X 已清空）里只有 R 有代码。

**信任以时间戳记账。** 接受判断、接受主动、接受回 call、继续纠正分别写入 JudgmentAcceptedAt / InitiativeAcceptedAt / CallbackAcceptedAt / CorrectionContinuedAt（implemented-at backend/internal/relationship/types.go:125-130；backend/internal/relationship/policy.go:246-255, 294-298）。

**熟悉感按需引用，不常驻。** 比赛事件写入 shared_moment 记忆（implemented-at backend/internal/relationship/memory.go:15-17）；只有用户发出召回线索（「想起/记得 + 上次那场」等，implemented-at backend/internal/relationship/policy.go:213-216）时，selectRelationshipMemories 才把它选进决策上下文（implemented-at backend/internal/relationship/memory.go:129-130）。未完话题记忆 14 天过期（implemented-at backend/internal/relationship/memory.go:33-37）。召回默认只给 recall；要附带调侃需 watch_buddy 及以上且有允许的调侃领域（implemented-at backend/internal/relationship/policy.go:149-154）。

**首见流程。** session_opened 记录 FirstMetAt 与 SharedMatches，首个决策是 ack（reason=`first_meeting_observed`，implemented-at backend/internal/relationship/policy.go:57-70）；首见问候语是一个 30 秒 TTL 的主动回合（implemented-at backend/cmd/server/main.go:842），送达后记录 GreetingDeliveredAt（implemented-at backend/internal/relationship/policy.go:49-56）。

**行为被测试锁定。** 20 个场景的一致性测试用真实信号序列构造各阶段并断言决策（eval：backend/internal/relationship/scenario_conformance_test.go:9-212；prepareStage 的升阶构造在 240-269）。

## 与旧设计的差异

- **升级驱动相反。** 旧文档：「阶段改变只允许由用户明确的设置动作触发，不得由连续登录、对话长度等推断」（旧稿 §1.1）。现实：升级完全由行为证据自动推断——场次计数+线索计数（implemented-at backend/internal/relationship/policy.go:310-319）。旧文档 §9.1 的场景「连续看十场但不保存偏好应停留在初见」在现实中不成立：看满场次并出现线索即升级。
- **阶段名称不同。** 旧稿「初见/已校准/共同观赛/长期默契」→ 代码「first_meeting/familiar/watch_buddy/old_ballmate」（implemented-at backend/internal/relationship/types.go:100-105）。
- **撤销只有一半。** 旧文档要求可随时删除、撤回（旧稿 §4.4）。现实禁话题过滤会跳过 RevokedAt 非空的边界（implemented-at backend/internal/relationship/policy.go:425-427；字段 backend/internal/relationship/types.go:181），但全仓库没有把关系边界 RevokedAt 写值的路径，用户也没有撤销入口——partial。
- **控制面缺位。** 旧文档 §8.1 要求设置首屏有「暂停连续性」「查看与编辑已保存内容」「重置为初见」。现实客户端设置页只有称呼、话痨程度、连续对话、字幕、声音五项（implemented-at client/lib/screens/settings_screen.dart:80-139）——暂停/查看/重置均为 concept。
- **许可制度不存在。** 旧稿 §5 的三层许可（本场/跨场/长期）、分项开关、到期复核、确认面板与「稍后再说」等价路径均无代码对应（concept）；现实中唯一按领域分项的许可是调侃领域映射（implemented-at backend/internal/relationship/policy.go:256-291）。
- **阶段变化没有用户可见日志页。** 旧稿 §4.5 的「陪看设置变化」页面不存在；阶段只体现在 Decision.RelationshipView 随决策下发（implemented-at backend/internal/relationship/director.go:91-100）。

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 四阶段状态机（真实命名与旧稿不同） | implemented-at | types.go:100-105；policy.go:302-320 | code |
| 升级靠行为证据（与旧稿相反） | implemented-at | policy.go:310-319, 322-342 | code |
| 9 类线索计数器作为证据 | implemented-at | types.go:154-165；policy.go:237-300 | code |
| 修复中阻止升级，3 回合完成 | implemented-at | policy.go:307-309, 139-148 | code |
| 信任时间戳四项 | implemented-at | types.go:125-130；policy.go:246-255, 294-298 | code |
| 共同瞬间按召回线索引用 | implemented-at | memory.go:15-17, 129-130；policy.go:213-216 | code |
| 未完话题 14 天过期 | implemented-at | memory.go:33-37 | code |
| 高阶段才允许带调侃的召回 | implemented-at | policy.go:149-154 | code |
| 边界撤销过滤有、撤销入口无 | partial | types.go:181；policy.go:425-427 | code |
| 控制面只有 5 项基础设置 | partial | settings_screen.dart:80-139 | code |
| 三层许可/暂停/清空/日志页/一键降级 | concept | 旧稿 §4/§5/§8；settings_screen.dart:80-139 | doc |
| 20 场景一致性测试锁定阶段行为 | implemented-at | scenario_conformance_test.go:9-212, 240-269 | eval |
| 首见 ack+30 秒 TTL 问候 | implemented-at | policy.go:57-70, 49-56；main.go:842 | code |
