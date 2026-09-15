---
id: 27-dialogue-expression
title: 数字球友对话与表达
source_chapter: docs/球球全套资料/4.产品设计/27-数字球友对话与表达设计.md
status_summary: { implemented: 11, partial: 0, planned: 0, concept: 1 }
---

# 数字球友对话与表达

本章管球球「说什么、何时沉默、如何修复」。真实程度：表达的行为规则已实现——说什么由一张确定性策略表决定，LLM 只负责把决定说成中文；旧文档的语用框架大多没有对应代码，但其「控制优先、待确认保留、修复回合」三类核心主张在代码里逐条能对上。

## 系统实际怎么工作

**决策与表达分离。** 每个回合先由关系策略表（`applyPolicy`）确定性选出沟通动作，再由语言实现层把动作 realize 成台词。语言实现层的系统提示词第一句就是边界声明：「只负责把已经决定好的沟通动作说成自然中文……不能改变沟通动作、关系阶段、事实、不确定性、边界、调侃许可或是否追问」（implemented-at backend/internal/companion/realize.go:35-42）。agent 拿到策略决定后：决定沉默就撤回已起草的回复（`relationship_chosen_silence`），决定发言才交给 realizer 或退回确定性底稿（implemented-at backend/internal/companion/agent.go:1056-1068）。

**动作词表。** 全部表达被归约为 10 种沟通动作：ack / analyze / ask / disagree / opinion / recall / react / repair / silence / tease（implemented-at backend/internal/relationship/types.go:263-276）。`applyPolicy` 按信号类型与用户线索命中唯一分支（implemented-at backend/internal/relationship/policy.go:48-187）。

**待确认不下结论。** 事实模式为 unverified 时，策略强制 react+disagree，reason 记为 `unverified_fact_requires_reserve`（implemented-at backend/internal/relationship/policy.go:158-160）。eval 黄金用例要求用户谎报进球时台词必须出现「还没跟上」、禁止出现「对，佩德里」（eval：evals/cases/boundary/user-claim-unverified.json）。

**控制层即时执行。** 「别说 / 少说 / 闭嘴 / 安静 / 别播报」被意图分类器命中为 control_command（implemented-at backend/internal/companion/agent.go:1798-1800），处理是固定回复「收到，我会少说一点，关键变化再提醒你。」，不进 LLM（implemented-at backend/internal/companion/agent.go:948-949）。

**安静是动作不是缺失。** 「不想分析 / 缓会儿 / 先别说话」被推断为 needs_silence 线索（implemented-at backend/internal/relationship/policy.go:204-206），策略产出 ActSilence（reason=`user_requested_quiet`，implemented-at backend/internal/relationship/policy.go:117-119），表演层同时切到 expression=low、motion=idle、voiceStyle=quiet、VoiceEnergy 0.2（implemented-at backend/internal/relationship/affect.go:80-87）。

**修复回合。** 「开我玩笑」「你怎么老说 / 又重复」「大道理 / 分析太多 / 太啰嗦」分别触发三类 RepairState（banter_boundary / repetition / over_analysis），行为约束含 reduce_initiative、reduce_questions 等（implemented-at backend/internal/relationship/policy.go:494-517）。修复期间表达被压到 2 句 40 字、清空调侃范围、脏话归零（implemented-at backend/internal/relationship/policy.go:394-404）；连续 3 个正常回合后修复完成并计入信任证据（implemented-at backend/internal/relationship/policy.go:139-148）。场景一致性测试 09/10/12 锁定该行为（eval：backend/internal/relationship/scenario_conformance_test.go:87-119）。

**比例感是硬数值。** 默认每次最多 2 句 80 字；分析请求放宽到 3 句 160 字（implemented-at backend/internal/relationship/policy.go:381-393, 405-408）；句数与字数作为约束字段传给语言实现层（implemented-at backend/internal/companion/realize.go:57-58）。

**提问克制是代码不是文风。** 允许问句仅当动作含 ActAsk（implemented-at backend/internal/relationship/policy.go:388）；稳定偏好追问还需关系≥familiar、唤醒度<0.7、且最近 4 个动作记录中没有 ask——约每 5 回合最多一次（implemented-at backend/internal/relationship/policy.go:179-181；测试 `TestStablePreferenceAllowsOneValuableQuestionPerFiveTurns`，eval：backend/internal/relationship/content_policy_test.go:42-72）。

**不附和攻击。** 意见冲突线索或侮辱词（废物/垃圾/蠢货/裁判瞎/人没了）触发 disagree+opinion（implemented-at backend/internal/relationship/policy.go:161-166, 552-559）。情绪表达与个人分享只拿一个轻量 react，未命中任何规则时默认 ack（implemented-at backend/internal/relationship/policy.go:167-169, 183-186）。

**复读规避。** 每次表达策略携带 RecentPhraseHashes 哈希列表，语言实现层据此避免重复近期说法（implemented-at backend/internal/relationship/policy.go:392；字段定义 backend/internal/relationship/types.go:380）。

## 与旧设计的差异

- 旧文档写「五层对话模型」（事实/解释/共情/控制/安全）作为逐句判断流程（旧稿 §2.1）。现实没有五层路由：只有一张策略表按线索命中分支（implemented-at backend/internal/relationship/policy.go:48-187）；控制层、待确认、修复有真实对应，其余各层是散文描述。
- 旧文档要求「发言前七项检查表」（授权/阶段/事实等级/模式/边界/比例/退出）逐项过门（旧稿 §4.4）。现实比例感、提问、事实保留是代码约束（implemented-at backend/internal/relationship/policy.go:179-181, 381-393, 158-160），但不存在用户可感知的检查表，也无「授权检查」——主动与否由策略直接决定。
- 旧文档的「人工评估量表八维度 + 十条验收门」（旧稿 §9.3、§10.3）没有对应评估配置；现有评估是 evals/cases 下 baseline/boundary/regression 三个场景集（concept，evals/cases/）。
- 旧文档要求「事实必须带状态标签和来源」。现实以 ForbiddenClaims（禁止新增比分/球员/事件/点球结论）+ RequiredAnchors 约束表达（implemented-at backend/internal/relationship/policy.go:383-385），粒度是禁止清单而非用户可见标签。
- 旧文档「用户说别说话后角色不能再索取关系回应」。现实控制命令走固定回复、安静线索走 ActSilence+表演降档，两条链路都不再产生追问（implemented-at backend/internal/companion/agent.go:948-949；backend/internal/relationship/policy.go:117-119）。

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 决策/表达分离，LLM 不能改动作与事实 | implemented-at | backend/internal/companion/realize.go:35-42；agent.go:1056-1068 | code |
| 10 种沟通动作由策略表选择 | implemented-at | types.go:263-276；policy.go:48-187 | code |
| 未证实事实保持保留 | implemented-at | policy.go:158-160；evals/cases/boundary/user-claim-unverified.json | eval |
| 控制指令即时执行固定回复 | implemented-at | agent.go:1798-1800；agent.go:948-949 | code |
| 安静请求→ActSilence+表演降档 | implemented-at | policy.go:117-119, 204-206；affect.go:80-87 | code |
| 修复回合三类触发与行为约束 | implemented-at | policy.go:494-517, 394-404, 139-148 | code |
| 长度硬上限 2 句 80 字 / 分析 3 句 160 字 | implemented-at | policy.go:381-393, 405-408；realize.go:57-58 | code |
| 提问克制（唤醒度+近 4 回合门） | implemented-at | policy.go:179-181, 388；content_policy_test.go:42-72 | eval |
| 侮辱/意见冲突不附和 | implemented-at | policy.go:161-166, 552-559 | code |
| 情绪/分享轻量 react，默认 ack | implemented-at | policy.go:167-169, 183-186 | code |
| RecentPhraseHashes 防复读 | implemented-at | policy.go:392；types.go:380 | code |
| 五层对话模型 / 七项检查表 / 评估量表与验收门 | concept | 旧稿 §2.1/§4.4/§9.3/§10.3；evals/cases | doc |
