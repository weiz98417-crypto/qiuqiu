# policy 三件:赛点主动档 + 话痨场景联动 + 用户语音情绪偏置

## Why

三个 policy 层小改,消费者都是当期陪看用户(ADR-0023 门全过):

1. **B2 赛点事件主动档**:proactive gate 只校验 C2 引用码非空+quiet 档压放(conversation/proactive_gate.go:47-63),事件白名单无权重——点球/红牌/决胜球与普通进球同级。Skyrim.AI「关键时刻主动提醒(this could decide the game)」验证:这是「AI 陪你看」区别于聊天机器人的本体功能;咪咕赛点识别同方向。
2. **B5 话痨档场景联动**:话痨三档(quiet/normal/active)是全局静态设置;Copperline 三档人格验证「话痨档是场景设置不是性格设置」——进球瞬间=短句快报,中场休息=畅聊。现状只差 policy 表的事件×档位矩阵。
3. **C2 useraffect 兑现**:SenseVoice 情绪信号已建(置信门 0.55、原子合并进语音 trace),但零行为消费者——CONTEXT.md 宣布的「可偏策 policy」悬空一个版本。观测面永远为 0 = 功能不存在(ADR-0023 反面)。用户裁决(grilling Q4):兑现,只接 policy 偏置一条腿。

## What Changes

- **B2**:matchstate 事件分类加「赛点」权重类(点球判罚中/红牌/领先一球进入最后 15 分钟/决胜球);proactive 优先级阶梯为赛点事件抬档;理由码 `proactive_citation:pivotal`;**quiet 档对赛点的放行单独裁决**(默认建议:quiet 档赛点仍放行但单句短播——在场≠打扰与赛点稀缺性平衡,实施时真机定)。
- **B5**:policy 表加事件场景×话痨档矩阵——进球瞬间全档短句快报(≤2 句)、中场休息 normal/active 畅聊、quiet 保持现状;矩阵进 policy 单源(presentation_table 同款纪律,数值可调)。
- **C2**:relationship policy 加 user_affect 偏置入口(仿 applyMemoryBias 先例 policy.go:121-127)——useraffect 信号(叹气/低落×球队落后)→ Communication Act 偏向安慰型而非解说型,带 `user_affect:` reason code 随行;**只偏置不支配**(置信门 0.55 沿用,信号缺席时行为与现状逐字节一致)。观测先行:先跑一周确认信号非零分布,再启用偏置(两步在同一 change 内,开关 config 控制)。

## Non-goals

- 情绪人格化全量(affect 向 persona/记忆扩散)——只接 policy 偏置一条腿。
- useraffect 信号进比赛事实或 Match Fact 语义(CONTEXT.md 纪律:永不成为比赛证据)。
- 话痨档 UI/设置页改动(三档不变,矩阵是后端消费侧)。
- backchannel 情绪化(v1.1 短 TTS 的 Affect 折算已备好但属 ADR-0016 留尾,不在本 change)。

## Success Criteria

- B2:赛点事件优先级/理由码 eval;quiet 档裁决落记录;
- B5:矩阵命中 eval(进球短句/中场畅聊/quiet 不变);
- C2:偏置 reason code 进 trace 与决策;信号缺席=现状(字节级);观测一周信号非零后 config 开偏置;
- A7 分摊:backchannel(限频/让路/风暴去重)+ useraffect(置信门/attach)+ proactive 端到端(提醒簿/订阅展开/过期转素材)evals 套件。
