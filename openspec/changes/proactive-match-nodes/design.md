# Design: Proactive Match Nodes

## 节点宿主对照

| 节点 | 性质 | 宿主 | 门 |
|---|---|---|---|
| 中场闲聊 | 在线事件时刻 | 事件反应路径（period 切换） | 让路/quiet/现有反应门 |
| 失球安慰 | 在线事件时刻 | 进球事件反应前置条件 | 订阅命中+每场≤1+quiet |
| 复盘邀约 | 时间点（FT+15min） | proactive Reminder 045 全套 | ADR-0015 引用码+FT+2h 过期 |

## 关键细节

- **halftime 语境拼装**：上半场事件来自 matchstate 快照（比分+事件列表截前 N 条），情绪取 Affect State 当前值，未完话题取 open threads 里 match 域活跃项——全部决策层已有，只拼 prompt。
- **失球判定**：进球事件带 scoring team；用户订阅球队集合（subscription store AllForUser）经 teamalign 归一比对；「失球方=对方进球且对方≠用户主队」与「用户主队被进球」取后者语义——安慰对象是**用户支持的队丢球**。
- **复盘邀约 Reminder 形状**：`kind=fulltime_review`，citation 沿用 `reminder:<id>`（ADR-0015 现成），素材引用当日 Shared Moment 的 Ledger 序列；过期转素材的 SweepSuppressed 已有语义，只需 kind 纳入。
- 复盘邀约的 offline 补递：045 outbox 天然支持；过期窗口 FT+2h 意味着重连晚于 2h 不再打扰。

## eval 断言

中场：period 切换后 N 秒内出现话轮（G 的状态条件等待）且 quiet 档静默；失球：命中订阅→安慰 act+reason code，未命中→常规反应；复盘：FT 后 reminder due、+15min 投递、+2h 过期 suppressed。
