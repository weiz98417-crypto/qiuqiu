# Design: Season Subscription

## 订阅簿与展开

- `Subscription`：`status active→cancelled`；引用码 `subscription:<id>`（proactive 包所有前缀，与 reminder 同款）。
- 展开器 `ExpandSubscriptions`（SweepLoop 同节拍外新增每日一次）：读活跃订阅 → 每订阅一次 14 天窗口扫描（GetFixturesContext 逐日，合并进每日 ≤2 次全窗预算）→ 队名双向 contains 命中且 kickoff 在未来 → 落 Reminder（`Kind: subscription`，去重键 = subscription_id+match_id，展开器对 **AllForUser 全量提醒**（含已投递/已静默）查重——仅查 pending 会让早场已投递的提醒被重复展开）。
- Reminder 增 `SubscriptionID` 字段（migration 047 一并加列）；引用码优先 `subscription:<id>`，投递文本同 PreMatchReminderReply。

## 管理意图

`subscription_manage`（注册表一处声明，置信门控）：子动作靠关键词分流（以后/每场+队名 → 订阅；列出/看下 → 列表；别叫/取消+队名 → 取消）。Handler 经 `a.subscriptions` Store：订阅走 teamNamesAlign 对齐 + 上限检查；列表确定性拼装；取消翻 cancelled（未投递的该队 reminder 同步 suppressed）。

## teamNamesAlign 收敛（还债）

（as-built）对齐下沉 proactive.TeamNameAligns：**别名表 TeamAliases**（皇马→皇家马德里等——口语短名不是全名的子串，纯 contains 对不上）展开后全等或互为包含；预备队/青年队/女足正则排除（曼联U21 不误配曼联）；kickoff 仅取未来且窗口 ≤14 天。用户文本→队名用 mentionsRunes（≥2 个不同队名字符）。

## WS 级测试补位（还债）

`submitDueReminders` 抽出可测核心（输入 pendings + delivery 假件 → 断言投递/翻状态/过期跳过），watchconnection 内保留薄壳。

## 删除测试

删掉订阅簿：订阅意图退"还没接上"，单次提醒不受影响。
