---
id: 31-initiative-silence
title: 主动发言、主动沉默与话痨控制
source_chapter: docs/球球全套资料/4.产品设计/31-主动发言、主动沉默与话痨控制设计.md
status_summary: { implemented: 12, partial: 0, planned: 0, concept: 2 }
---

# 主动发言、主动沉默与话痨控制

本章管球球「什么时候自己开口、什么时候闭嘴、吵不吵」。真实程度：冷却、用户抢占、安静线索、主动沉默都已实现；但旧文档的两根支柱——三层发言预算与四类主动触发+单项许可——是虚构，现实是「冷却时间戳 + 操作模式/事件白名单」这一套更简单的机制。

## 系统实际怎么工作

**资格门有四个条件。** 一个公开比赛事件能否触发主动发言，取决于 `proactiveGate.Allow`：比赛自动化策略的 Mode 为 active；事件类型在策略的 EventTypes 白名单里；**必须携带非空引用**——`open_thread:<id>`（Open Thread 台账）或 `shared_moment:<eventId>`（共同瞬间），无引用不出手（implemented-at backend/internal/conversation/proactive_gate.go:31-50）；用户话痨档位（quiet 屏蔽非关键主动，只收不放）。引用码随 trace `response.emit_companion_reply` 参数、`Decision.ReasonCodes`（`proactive_citation:<code>`）与 WS presentation 消息的 `citation` 字段三处可审计。main.go 在事件流上取策略、判定紧急度后过门，再把结果作为 OutputAllowed 传给 companion agent（implemented-at backend/cmd/server/main.go:738-761）。

**事件标签覆盖。** 事件带 `proactive=quiet` 标签直接不允许；带 `proactive=manual` 标签走 AllowManual（恒真），供导播手写话术通道使用（implemented-at backend/cmd/server/main.go:749-753；proactive_gate.go:19-21）。

**话痨控制是冷却时间戳。** InitiativeBudget 结构只有三个字段：Mode、LastNormalAt、LastCriticalAt（implemented-at backend/internal/relationship/types.go:222-226）——是冷却时间戳，不是发言配额。非关键事件距上次普通发言不足 90 秒（或策略的 CooldownSeconds，经 main.go:763 传入）时，策略产出 ActSilence，reason 记为 `natural_initiative_cooldown`；关键事件刷新 LastCriticalAt 并放行 react（implemented-at backend/internal/relationship/policy.go:79-92）。

**用户回合抢占。** 用户一说话，SubmitUser 取消正在进行的主动回合并把用户回合插到队列最前（implemented-at backend/internal/conversation/scheduler.go:198-206；调用点 backend/cmd/server/main.go:815）。用户打断即抢断，主动内容还没播就作废。

**去重与过期。** 主动回合按 dedupe key + TTL 去重：同 key 未过期直接丢弃；排队中过期的回合出队时静默删除（implemented-at backend/internal/conversation/scheduler.go:186-197, 139-147）。TTL 常态 30 秒、关键事件 90 秒（implemented-at backend/internal/relationship/policy.go:387-393；main.go:2537 的首见问候也是 30 秒）。

**紧急度插队。** 队列按紧急度插入：goal/red_card/penalty/var_check/goal_cancelled/halftime/fulltime 等归为 critical，插到普通主动回合之前（implemented-at backend/internal/conversation/scheduler.go:207-218；分类表 backend/cmd/server/main.go:2528-2531）。

**播放状态机。** 主动回合注册播放后，若 90 秒（默认 PlaybackTimeout）内播放未真正开始则回收；播放变为非 started、超时或外部 interrupt 都终结该回合并启动下一个（implemented-at backend/internal/conversation/scheduler.go:70-72, 221-262）。

**主动沉默是决策。** 「不想分析 / 缓会儿 / 先别说话」被推断为 needs_silence 线索（implemented-at backend/internal/relationship/policy.go:233-235），策略产出 ActSilence（implemented-at backend/internal/relationship/policy.go:132-135），表演层同步降到 low/idle/quiet（implemented-at backend/internal/relationship/affect.go:80-87）。沉默还会撤回已起草的内容：策略只产出 silence 时 SpeechPlan 为 nil，agent 丢弃已生成的回复并记录 `relationship_chosen_silence`（implemented-at backend/internal/relationship/policy.go:383-386；backend/internal/companion/agent.go:1163-1166）。OutputAllowed=false 的事件同样直接 ActSilence（implemented-at backend/internal/relationship/policy.go:73-75）。

**被纠正就降主动。** 「别拿…开我玩笑」（banter_boundary）和「大道理/分析太多/太啰嗦」（over_analysis）两类修复的行为约束都含 reduce_initiative（implemented-at backend/internal/relationship/policy.go:529-552）；修复期间表达压缩到 2 句 40 字、调侃清零（implemented-at backend/internal/relationship/policy.go:433-438）。

**手写话术保护。** 导播在事件上写的 proactiveText 走 manual 通道，eval 用例要求逐字送达、不被改写（eval：evals/cases/regression/manual-proactive-line.json）。

## 与旧设计的差异

- **三层发言预算是虚构。** 旧文档设计了「单个事件预算/时段预算/整场预算」三层配额（旧稿 §6.1-6.3）。现实中不存在任何配额计数器：话痨控制只有「距上次普通发言的冷却时间」（implemented-at backend/internal/relationship/types.go:222-226；policy.go:79-92）。时段差异、整场上限、「预算接近用尽默认沉默」均无实现（concept）。
- **四类主动触发+单项许可是虚构。** 旧文档把主动拆成安全状态/用户请求/已许可节奏/有限回顾四类，每类要求用户单项许可（旧稿 §1.1、§2、§5）。现实没有用户主动许可的分类体系：资格门 = 操作模式 + 事件类型白名单 + 引用码 + 话痨档位（implemented-at backend/internal/conversation/proactive_gate.go:31-50），再加事件标签 quiet/manual 覆盖（implemented-at backend/cmd/server/main.go:749-753）。
- **冷却期的范围比旧文档窄。** 旧文档要求「类别级冷却、关闭后不得换通道续打」（旧稿 §6.4）。现实冷却是全场单一时间戳，不分类别也无跨通道概念（implemented-at backend/internal/relationship/policy.go:79-85）。
- **用户安静线索已实现且更强。** 旧文档要求「先别说」立即覆盖一切；现实安静线索不仅让策略沉默，还会撤回已起草的回复并同步压制表演层（implemented-at backend/internal/relationship/policy.go:132-135；agent.go:1163-1166；affect.go:80-87）——这一点实现比旧文档的具体机制更彻底。
- **「话痨程度」设置已完成接线（2026-09-17）。** 审计时它还是半成品——客户端三档随 user_speech 上送但后端不读（implemented-at client/lib/screens/settings_screen.dart:93-108）。agent-depth 修复：后端解析并按用户持久化到 user_preferences 表（implemented-at backend/cmd/server/main.go:1077-1091, 997-1004；backend/migrations/040_open_threads.sql:9-14），三档映射主动频率——quiet 屏蔽非关键主动、normal 保持 90s 基线、active 冷却 ×0.6=54s（implemented-at backend/internal/relationship/talkativeness.go:1-60）；重连经 session_opened 恢复。quiet 只收不放，L0 安全。
- **主动回合的工程保障比旧文档细。** 旧文档没有涉及：去重 key+TTL、紧急度插队、播放超时回收、用户抢占，这些在 scheduler 里全部实现（implemented-at backend/internal/conversation/scheduler.go:186-262, 198-206）。

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 资格门 = 模式 + 白名单 + 引用码 + 话痨档位 | implemented-at | proactive_gate.go:31-50；main.go:738-761 | code |
| proactive=quiet/manual 事件标签覆盖 | implemented-at | main.go:749-753；proactive_gate.go:19-21 | code |
| 冷却是时间戳不是配额（90s 默认） | implemented-at | types.go:222-226；policy.go:79-92；main.go:763 | code |
| 话痨三档接入主动性控制（漂移已修复） | implemented-at | main.go:1077-1091, 997-1004；talkativeness.go；migrations/040:9-14 | code |
| 用户回合抢占主动回合 | implemented-at | scheduler.go:198-206；main.go:815 | code |
| 主动去重 key+TTL、过期丢弃 | implemented-at | scheduler.go:186-197, 139-147 | code |
| critical 主动回合插队 | implemented-at | scheduler.go:207-218；main.go:2528-2531 | code |
| 播放注册/超时/打断状态机 | implemented-at | scheduler.go:70-72, 221-262 | code |
| 安静线索→ActSilence+表演降档 | implemented-at | policy.go:132-135, 233-235；affect.go:80-87 | code |
| 沉默撤回已起草回复 | implemented-at | policy.go:383-386；agent.go:1163-1166 | code |
| 修复行为含 reduce_initiative | implemented-at | policy.go:529-552, 433-438 | code |
| 三层发言预算/类别级冷却 | concept | 旧稿 §6；types.go:222-226 | doc |
| 四类主动触发+单项许可分类 | concept | 旧稿 §1.1/§2/§5；proactive_gate.go:15-21 | doc |
| 手写主动话术逐字播出 | implemented-at | evals/cases/regression/manual-proactive-line.json；main.go:751-753 | eval |
