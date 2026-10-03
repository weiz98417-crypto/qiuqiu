# Design: 自动托管

## D1 · 稳定窗按时间不按轮数

「连续两轮仍在」的朴素定义在双源下失效:ESPN 秒级轮询两轮只隔几秒,api-sports 15-60s 一轮。稳定窗定义为**事件首次入账后 ≥N 秒(默认 30s,config 可调)仍在源快照中、且期间无内容 diff、无跨源冲突**才自动 confirm。30s 的依据:调研口径「自助付费轮询档进球延迟 15-60s」+ VAR 时代改判高发窗(进球→VAR 结论通常 <60s)——窗口太短会把将要被取消的进球确认掉(可被 correction 追加修正,但用户已听过庆祝),太长伤实时性。首窗 30s,真实数据后调。

自动确认的「操作者」记 source 名(如 `auto:espn+stability30s`),审计可追溯——不冒充人工。

## D2 · 两种 source 形态并存

api-sports = 事件游标模型(增量拉事件,游标持久化);ESPN = 快照 diff 模型(拉 scoreboard 全量,前后 diff 产事件)。Manager 的 Source 枚举扩展后,poller 循环对两种形态各自适配:游标型照旧,diff 型维护「上次快照」状态(进程内,重启冷启动一次全量 diff 须幂等——事件按 ProviderEventID `espn:<matchKey>:<detailSeq>` 幂等,store.go:642-648 已按 Source+ProviderEventID 去重,diff 重放安全)。

ESPN 的 `details[]` 是覆盖式返回无变更语义(调研核实)——diff 是客户端责任,这正是 adapter 的核心工作:进球/红黄牌/换人/比分/状态机(SCHEDULED/IN_PLAY/…)五类 diff 规则。

## D3 · ADR-0002 修订边界(ADR-0024)

不动:「外部数据进账本先落 provisional」「账本只进不改、快照不可 patch」「LLM 永不生成事实」。
动:provisional → confirmed 的迁移从「仅运营」扩为「运营 或 (信任表允许的自动确认)」。信任表初始值:

| 事件类 | 自动确认 | 理由 |
|---|---|---|
| goal / red_card / yellow_card / substitution / period 切换 | ✅(过稳定窗) | 稳定类,错了可 correction 追加 |
| var_check(VAR 进行中) | ❌ 永远运营 | 过渡态,天然不稳定 |
| goal_cancelled / score_correction | ❌ 永远运营 | 改判是话术敏感事件,人工把关 |
| 冲突挂起(双源不一致) | ❌ | 仲裁本身是运营职责 |

表进 config(默认值硬编码 + env 覆盖),运营台只读展示。

## D4 · 改判盲区的实现形状

poller 每轮对「已入账但源已变化的事件」做三件事:内容 diff(比分/球员/分钟变化)→ 产出对应 provisional 修正事件;事件从源快照消失 → 登记待裁决冲突(不自动 revoke,静默消失最危险);`Var` 事件解析结果字段(ESPN details 有 VAR 语境)→ 区分「检查中/维持/改判」,改判才产 goal_cancelled。所有自动产生的修正事件类都在信任表的 ❌ 列——**修复盲区 ≠ 放开改判自动确认**,两件事分开。

## D5 · 一键托管此场的编排

运营台「今日赛程」列表(数据源=已有 schedule reader)→ 选场 → 单一编排调用:建场(match config 从 fixture 填充)+ start + sources/start(默认 ESPN 主源;api-sports key 在配时自动挂二源)+ 订阅展开即时跑一次。编排走 executeOperatorWrite 幂等信封(既有纪律)。终场自动停源(状态机 FINISHED 驱动),不留悬挂轮询。

## D6 · ESPN 无 SLA 的对冲

三层:双源仲裁(D2 的不一致挂起)、健康探测连续失败自动切「仅 api-sports」或停源告警(运营台 LiveMonitor 可见)、ProviderEventID 幂等保证切换/重试不双记。ESPN 端点变更(非官方风险)表现为连续 4xx/结构变化——探测面板显形,不静默。
