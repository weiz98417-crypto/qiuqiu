# Season Subscription: 订阅提醒与赛季陪伴

## Why

提醒限于单场单次；"以后皇马的比赛都叫我"无法表达。MLS Sidekick 式赛季习惯是球迷产品最低预期，而提醒簿（能力波 #4）已把单次做对——订阅是自然延长线，同时为多比赛支持铺跨场连续性地基。

## What Changes

- `internal/proactive` 增订阅簿：`Subscription{ID, UserID, TeamName, CitationCode}`, migration 047；Store 双实现（Postgres/内存）。
- 展开节拍（每日合并扫描 ≤2 次全窗，Q17）：订阅 → 扫未来 14 天该队赛程（GetFixturesContext 逐日窗口）→ 为未提醒过的比赛落 Reminder（kind=subscription，引用码 `subscription:<id>`，Q8）→ 新赛程发布即增量补展开。
- 管理意图 `subscription_manage`（对话即接口零 UI，Q9）："以后皇马的比赛都叫我"订阅 / "列出我的订阅" / "别叫我皇马的了"取消；**每用户上限 3 支**（Q18），超出提示先取消。
- 顺路还债：teamNamesAlign 同名前缀风险收敛 + submitDueReminders 的 WS 级测试补位。

## User Stories

1. As a 皇马球迷, I want 说一句"以后皇马的比赛都叫我", so that 整个赛季我不错过任何一场。
2. As a 用户, I want 一句话列出/取消订阅, so that 管理不用翻设置页。

## Non-goals

- 真推送（厂商 push）；投递面仍为 App 内。
- 多场比赛同时陪看（演进计划在册，本 change 只铺提醒侧地基）。

## Success Criteria

- 全量 go test + eval 绿；订阅展开/上限/取消有单测；teamNamesAlign 修正有单测。
