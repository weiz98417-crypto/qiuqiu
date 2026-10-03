# Tasks: policy 三件

- [ ] 4.1 B2 赛点分类:matchstate 事件→赛点权重类(点球判罚中/红牌/决胜时段),proactive 优先级抬档 + `proactive_citation:pivotal`;evals:优先级/理由码。
- [ ] 4.2 B2 quiet 档裁决:默认「赛点放行但单句短播」,真机验证后定稿记录进 tasks。
- [ ] 4.3 B5 场景矩阵:事件场景×话痨档矩阵进 policy 单源(进球短句快报/中场畅聊/quiet 现状);evals:矩阵命中。
- [ ] 4.4 C2 观测先行:useraffect 信号分布观测一周(快修轮的 ClientHealthLedger/trace 面),信号非零确认记录。
- [ ] 4.5 C2 偏置接线:relationship policy user_affect 入口(仿 applyMemoryBias,`user_affect:` reason code,0.55 门,config 开关默认关→观测确认后开);evals:偏置命中/信号缺席=现状字节级。
- [ ] 4.6 A7 evals(本 change 分摊):backchannel 套件(限频/让路/风暴去重)+ useraffect 套件(置信门/attach 合并)+ proactive 端到端套件(提醒簿/订阅展开/过期转素材)。
- [ ] 4.7 门禁:go 全量 + evals + pr tier。

## Sequencing

波2,与 agent-internals 并行(交叉点:4.1/4.5 接 policy 阶梯,若 agent-internals 3.3 管线命名化已落则接命名阶段,soft 依赖)。C2 观测先行(4.4)与 4.5 串行(一周窗),其余并行。B2 依赖 auto-hosting 的真实赛点数据做真机裁决(4.2)——若波1B 未 soak 完,4.2 用注入事件先裁决、真实数据复验。
