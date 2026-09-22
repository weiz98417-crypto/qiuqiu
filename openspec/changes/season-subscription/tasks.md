# Tasks: Season Subscription

- [x] 4.1 migration 047（subscriptions 表 + reminders.subscription_id 列）+ Subscription Store 双实现。
- [x] 4.2 展开器（每日合并扫描 + 去重落 Reminder）+ teamNamesAlign 收敛 + 单测。
- [x] 4.3 subscription_manage 意图（注册表 + handler：订阅/列表/取消/上限）+ 单测。
- [x] 4.4 teamNamesAlign 收敛 + 单测。submitDueReminders 的 WS 壳测试如实留尾（负反馈面板层未动，逻辑经 ExpandSubscriptions 纯函数与守卫单测覆盖）。
- [x] 4.5 CONTEXT.md「订阅」词条。
- [x] 4.6 验证：全量 go test + eval 绿。

## Sequencing

第二波第 3 个；依赖 proactive（能力波 #4）与 intent-registry。
