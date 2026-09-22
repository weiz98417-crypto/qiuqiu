# Design: Polish Round 3

## T1 审计收口

- 落账点在**入口层**（watchconnection `set_character` 分支 + character_api PATCH 分支），经 `agent.RecordInteraction` 风格的公开方法写 Ledger（service 保持纯净，不知道 Ledger 存在）；事件 `character.updated`，字段 field/from/to/source（ws|http|cue）。
- console 只读段：`/api/console/users/{id}` 读形状附 `preferences` 段（从 CharacterSettingStore.Get 读；无库部署返回空串槽位）。不新增写面。

## T3 上限下沉

`SubscriptionStore.Append` 双实现内计数活跃订阅，`>= MaxSubscriptionsPerUser` 返回 `ErrSubscriptionLimit`；handler 捕获该错误输出友好提示（现有"最多订三支球队"文案不变，触发点从读-判-写变为 store 拒绝）。

## T4 投递腿测试

`PlanDue(pendings []Reminder, now) []Reminder`：过滤 `Due(now)` 并按 DeliverAt 升序。`submitDueReminders` 改为先 PlanDue 再逐条投递；PlanDue 单测覆盖到点/未到点/已过期/已投递/排序。

## T2-cue 词迁移

policy 命中设置类触发词（主动性/分析胃口/调侃许可）时，除写 relationship_state 外，同步落 user_character_settings（经 cmd/server 侧钩子，service 不知道 settings 表）；话痨档维持原表（行为不变）。使三入口从"两个新入口 + 一个旧入口"收敛为"一份状态 + 三条路"。

## 词汇

不新增 CONTEXT.md 词条（订阅/知识条目已录；互动规范是既有概念的实施层）。
