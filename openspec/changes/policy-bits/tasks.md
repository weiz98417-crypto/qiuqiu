# Tasks: policy 三件

- [x] 4.1 B2 赛点分类:matchstate 事件→赛点权重类(点球判罚中/红牌/决胜时段),proactive 优先级抬档 + `proactive_citation:pivotal`;evals:优先级/理由码。
  - 分类器 `conversation.IsPivotalMatchEvent`(penalty_awarded/penalty/red_card 按类型;goal 按时钟 ≥75' + 事件后一球差,账本投影比分优先);队列档 `UrgencyPivotal`(scheduler 按 urgency 插队,decision 明示降级 normal 时不夺回);引用码固定 `CitationPivotal` → 决策理由码 `proactive_citation:pivotal`。evals:pivotal_test.go(分类 10 例+gate)、scheduler_test.go(插队)、policy_bits_test.go(理由码)。
- [x] 4.2 B2 quiet 档裁决:**已按默认值实施并落记录——「赛点放行但单句短播」**:gate 放行(conversation),单句预算 clamp 在 relationship contentPolicyFor(1 句/40 字,对矩阵与 act 调整保持权威)。真机验证后定稿,若有修订改 clamp 数值即可。
- [x] 4.3 B5 场景矩阵:事件场景×话痨档矩阵进 policy 单源(进球短句快报/中场畅聊/quiet 现状);evals:矩阵命中。
  - `relationship/scenario_matrix.go` 数据表+查表函数(presentation_table 同款纪律):goal 全档 2 句/80 字(有意钉住)、halftime normal/active 4 句/200 字、halftime quiet 与未知场景落默认格=现状;消费点 contentPolicyFor,用户话轮不经矩阵。evals:scenario_matrix_test.go 钉格+集成。
- [ ] 4.4 C2 观测先行:useraffect 信号分布观测一周(快修轮的 ClientHealthLedger/trace 面),信号非零确认记录。
  - **观测面已备,一周窗自本 change 上线起算**:relay 新增 Accepted 计数 + 过门结论结构化日志一行(`user affect signal: ...`),分布看「计数/日志/语音 trace 的 Voice.UserAffect」三面;非零确认后开 `QIUQIU_USER_AFFECT_POLICY_BIAS`。
- [x] 4.5 C2 偏置接线:relationship policy user_affect 入口(仿 applyMemoryBias,`user_affect:` reason code,0.55 门,config 开关默认关→观测确认后开);evals:偏置命中/信号缺席=现状字节级。
  - `relationship/user_affect_policy.go`:改道只发生在战术问答出口(解说型 ActAnalyze → 安慰型 ActReact,理由码 `user_affect:comfort_over_analysis`),四取齐门(载荷在场/标签低落闭集/置信 ≥0.55/支持队落后);开关 `QIUQIU_USER_AFFECT_POLICY_BIAS` 默认关=载荷不上话轮。WS 装配:relay.Latest(新鲜窗 2min)+画像/快照回源 TeamBehind。evals:user_affect_policy_test.go(命中/缺席字节级/四门)+policy_bits_test.go(载荷流转)。
- [x] 4.6 A7 evals(本 change 分摊):backchannel 套件(限频/让路/风暴去重)+ useraffect 套件(置信门/attach 合并)+ proactive 端到端套件(提醒簿/订阅展开/过期转素材)。
  - **复查结论:三套件既有覆盖已足,不另起炉灶**——backchannel(限频/半场帽/10s 风暴去重/quiet+user speaking 让路)= backchannel_test.go 四测;useraffect(置信门/bind 两序合并/旁路静默/事实账本隔离)= useraffect_relay_test.go 四测;proactive(提醒簿时序/due sweep/订阅展开去重/store 帽)= proactive_test.go + subscription_test.go。本 change 新增缺口只在 B2/B5/C2 三面,见上。
- [x] 4.7 门禁:go 全量 + evals + pr tier。go 全量 33 包绿;evals/pr tier 随 CI(evals.yml)。

## 接线顺手修复(同 PR 单独 commit)

- **存量 bug**:WS 语音路径的 Settings(ADR-0018 粘性覆盖)在 completeVoiceSessionWithOptions 处被静默丢弃——readPreferenceOverrides 每回合读了、voiceSessionOptions 带了,却从未转发进 MessageRequest,用户显式设置在主力路径从未生效。已补转发 + 流转断言测试(voice_options_forwarding_test.go)。

## 双轴审查留尾(code-review 2026-10-06,均已裁定)

- **状态型赛点缩小(声明过)**:spec「领先一球进入最后 15 分钟」是状态,实现只判事件时点(75'+ 的一球差进球)——60' 1-0 保持到终场全程不触发赛点。事件驱动架构下状态型赛点需要 clock-tick beat,登记为增量候选,不在本 change。
- **quiet 双门已对齐(review 修正)**:gate 与 relationship 的 quiet 门同开同关(均看 Critical+Pivotal),消除「未来非 critical 赛点类型 gate 放行、policy 层静默」的层间不一致。
- **五层手工转发(Shotgun Surgery)**:Settings/UserAffect 经 voiceSessionOptions→completeVoiceSessionWithOptions→MessageRequest→AgentBoundaryRequest→UserSignal 五层穿线,Settings 静默丢失即其症状(已修+流转测试);链本身的结构性收敛(单 rides-along 结构)留尾。
- **favorite_team 字面量四处 + 画像提取重复**:config.go/memory_signals.go/watchconnection.go 各自提 favorite_team,watchconnection.favoriteTeamBehind 与 companion.memorySignals 形状重复——收 memory 包常量+助手,留尾。
- **Allow 7 参(Data Clumps)**:critical/pivotal/talkativeness 结伴,可捆 flags 结构;gate 测试刚全量适配,重构收益边际,留尾。
- **双置信门常数**:relay 落 trace 门(config UserAffectMinConfidence)与 policy 偏置门(UserAffectBiasMinConfidence=0.55)同值双源,注释各自声明管各自门;与「reason 码双端硬编码单源化」同类留尾。
- **观测面补强(review 修正)**:分布日志不打用户标识(情绪标签比 id 敏感,一周分布观测不需要 id),打 label/confidence/accepted/dropped;Accepted 计数进 console/health 面留尾。

## Sequencing

波2,与 agent-internals 并行(交叉点:4.1/4.5 接 policy 阶梯,若 agent-internals 3.3 管线命名化已落则接命名阶段,soft 依赖)。C2 观测先行(4.4)与 4.5 串行(一周窗),其余并行。B2 依赖 auto-hosting 的真实赛点数据做真机裁决(4.2)——若波1B 未 soak 完,4.2 用注入事件先裁决、真实数据复验。**本轮 4.2 已用注入事件按默认值实施,真机复验待用户侧托管比赛。**
