# Tasks: Reflection Attribution

- [ ] 1.1 per-user 互动过的比赛记录（数据源：现有 citations/ActiveUsers 附近的按用户账本序列，不建新表）。
- [ ] 1.2 runReflectionBeat 改为按用户×其观看的终场比赛分组触发；多场全消费。
- [ ] 1.3 单测：双场双用户归因、单场单用户不回归、无记录用户不触发。
- [ ] 1.4 验证：全量 go test 绿。

## Sequencing

架构评审 C 项（共识：~0.5 人日小修）。F 的复盘邀约引用 Shared Moment 依赖本项的红利（FT+15min 时反思已按对准的比赛冲洗），建议先于 F。
