# Design: Proactive Scheduler

## 形状

`internal/proactive`：
- `Reminder` + `NewReminder`（DeliverAt/ExpireAt 由 kickoff 推导：kickoff−30min / kickoff+30min）；
- `Store` 接缝：`Append / PendingForUser / DuePending / MarkDelivered / SweepSuppressed`，双实现 PostgresStore（migration 045，pgxpool）+ MemoryStore；
- `SweepLoop`：服务端唯一全局节拍——过期待递翻 suppressed + 转记忆素材（Q13）。到点投递不在此做（socket 归连接所有）。

投递两腿（均在连接内）：
- 连接时补递：`submitDueReminders(userID)` 挂 identify-and-subscribe 相（与 020/040 的连接恢复并列）；
- 连接内 30s ticker（`startReminderTicker`）：覆盖"一直挂在 App 里跨过开球时刻"的主场景。
两腿共用同一投递体：确定性文本（`PreMatchReminderReply`）+ `relationship.PresentationPlan`（calm）+ `SubmitProactive(key="reminder:<id>", UrgencyNormal, ttl=至过期)`，送达才 MarkDelivered。

## 意图（注册表首批用户）

- 词表：开球前叫我/提醒我/叫醒我、开赛前/比赛前/赛前 ×叫我/提醒我、提前叫我（classify 管道步骤插在 silence_request 之后）。
- router：`reminder_request` 枚举 + prompt 意图定义行（插在 control_command 与 unknown 之间——枚举序=注册表声明序=校验锁）。
- handler：`pre_match` 且 `reminderKickoff`（搜索窗口昨天→后天，降级今日赛程，队名双向 contains 且非空对齐）才落簿；非 pre_match / 无 kickoff / 未接簿 → 如实回话不落簿。确定性回复、`allowRealize=false`、`deterministicReason="reminder_policy"`、trace 盖 `reminder_scheduled` 与 `reminder.create` ToolCall。

## 引用码与门

`reminder:<id>` 由 proactive 包所有（conversation ← companion ← proactive 成环，故不进 conversation）；gate 语义不变（非空即过）。ADR-0015 记录第三钥匙的信任语义与被否决项。

## 素材化（Q13）

SweepLoop 把 suppressed 提醒转为 `memory.Moment`（MomentUserFact，内容含两队名——contains 检索可达），重要性 0.6；下一次相关聊天/主动回合的 recall 材料里自然出现。

## 删除测试

删掉 internal/proactive：提醒簿、第三钥匙、时间维度主动性全部消失，agent 退回"没人连接就没有主动回合"——本包承载的是新领域能力而非薄封装。
