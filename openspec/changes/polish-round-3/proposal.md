# Polish Round 3: 信任与正确性还债

## Why

两波能力升级（能力波 #1-4、第二波 #1-5）留下四笔信任/正确性债：character-settings 无 Ledger 记账与运营台可见性（审计完整性）；订阅上限只在 handler 层强制（并发 TOCTOU）；submitDueReminders 无可测核心与测试（投递腿零覆盖）；cue 词入口仍写旧状态（三入口一状态未收敛）。三笔是信任债不是卫生债，拖久会变成上线后事故。

## What Changes

- **T1 审计收口**：character-settings 三入口成功后记 Interaction Ledger 事件 `character.updated`（field/from/to/source，入口层落账、service 保持纯净）；`/api/console/users/{id}` 读形状附 preferences 只读段；一致性/记账单测。
- **T3 订阅上限下沉**：`MaxSubscriptionsPerUser` 校验下沉到 SubscriptionStore.Append（双实现），handler 保留友好提示；并发超限被 store 层拒绝。
- **T4 投递腿测试**：抽取 `proactive.PlanDue(pendings, now)` 纯函数（到点筛选 + 排序），submitDueReminders 改用之；PlanDue 单测（到点/未到点/过期/排序）。
- **T2-cue 词迁移**：设置类 cue 命中后写入 user_character_settings（与 WS/HTTP 同一状态），cue 词与 WS/HTTP 真正共用一份状态——三入口一状态收敛。

## User Stories

1. As a 运营员, I want 干预台看到用户当前互动规范, so that 干预有依据。
2. As a 用户, I want 用说话调的设置和设置页改的是同一份状态, so that 不出现"说了没生效"。
3. As a 守门人, I want 并发订阅也无法绕过上限, so that 防滥用不被时序击穿。

## Non-goals

- 不改任何 policy 运行时行为（设置生效 = settings-in-policy，另行立项）。
- 不动决策层（memory-in-policy 另行立项）。
- 无新功能、无新端点。

## Success Criteria

- 全量 go test + eval 绿（本波零行为变化）。
- Ledger 记账、console 只读段、store 层上限、PlanDue 各有单测。
