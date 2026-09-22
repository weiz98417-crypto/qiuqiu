# Tasks: Polish Round 3

- [x] 1.1 T1 入口层 Ledger 记账（WS + HTTP + cue 钩子三处）+ `character.updated` 事件。
- [x] 1.2 T1 console 只读 preferences 段。
- [x] 1.3 T3 订阅上限下沉 Store（双实现 + ErrSubscriptionLimit）+ handler 适配。
- [x] 1.4 T4 PlanDue 纯函数抽取 + submitDueReminders 接线 + 单测。
- [x] 1.5 T2-cue 设置类触发词落 user_character_settings + 单测。
- [x] 1.6 一致性/记账单测（三入口同一状态断言）。
- [x] 1.7 验证：全量 go test + eval 绿（零行为变化）。

## Sequencing

打磨轮 3 先行；settings-in-policy、memory-in-policy、knowledge-players 依次随其后（各自 spec 已立）。
