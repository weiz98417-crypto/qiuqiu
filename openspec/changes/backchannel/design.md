# Design: Backchannel

## 决策器

`internal/backchannel`：`Decide(eventType, state) (Utterance, bool)`。状态 = 本场已发计数（半场/全场）、当前 talkativeness、手动占用手感（有进行中回合或最近 2s 内有投递则跳过）。白名单与上限常量策展于包内词表文件（运营可改点集中一处）。短语池按事件类型分桶，随机取（math/rand 已 seeding 场景，取轮转避免连发重复）。

## 载体（F4）

投递走既有 `ResponseDeliveryService.Deliver` 的文本通道：Reply=短语（≤10 字）、Trace（独立 trace，Reason="backchannel"）、Presentation=当前 affect 象限的轻量表演（复用 30 槽位，celebrate_02/miss/complain/tense 四槽正好对应白名单事件）。客户端空 text+presentation 已按"只上表演"处理（开场 hello 同款），文字气泡字段沿用 Input 通道——若客户端把空 text 短语显示为气泡需要微改，则 v1 退化为纯表演+音频延后（grilling 已裁：文字气泡 v1 必须，表情槽位复用现有）。不进回合调度器——独立直发（限频表就是它的调度），失败即弃（微反应永不重试）。

## ADR-0016 主体

微反应不是发言：不占回合槽、不过 C2 引用码门、不进 open_thread/observation 管线；信任约束换形——独立限频 + quiet 档禁用 + 全量审计（trace+Ledger）。这是 C2 门 2026-09 以来的第一次显式扩张，理由：引用码门约束的是"有据可引的观点输出"，微反应是共情身体性，本质更接近表情表演（ADR-0005/0007 管辖）而非沟通输出。

## 删除测试

删掉 internal/backchannel：微反应消失，回合管线原样——独立通道，无悬空依赖。
