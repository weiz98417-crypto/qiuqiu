# Tasks: Proactive Scheduler

- [x] 4.1 internal/proactive：Reminder/Store 双实现（Postgres 045 + Memory）/SweepLoop。
- [x] 4.2 reminder_request 意图：词表、router 枚举与 prompt 行、注册表五件套、handler（reminderKickoff 队名对齐）。
- [x] 4.3 投递两腿：连接时补递 + 连接内 30s ticker（submitDueReminders/startReminderTicker），送达翻 delivered。
- [x] 4.4 main.go 装配：store 选择（Postgres/内存）、SweepLoop、agent.WithReminders、watchDeps.reminders。
- [x] 4.5 ADR-0015 + CONTEXT.md「主动调度」词条。
- [x] 4.6 单测：提醒簿时序/清扫、意图落簿三态、注册表 flag 回归；router 枚举锁 11→12。
- [x] 4.7 验证：全量 go test 绿 + 100 eval 绿。
- [ ] 4.8 （后置，独立技能）订阅式提醒（关注球队每轮自动 + 列出/取消管理面）。

## Sequencing

能力波第 4 个（旗舰收口）：依赖 #1 注册表（新意图一处声明）、#2 记忆（素材化落 recall、主动回合可带记忆）、#3 结构化缝（后续技能的解析落点）。
