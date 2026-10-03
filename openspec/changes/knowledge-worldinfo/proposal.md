# 知识域 World Info 参数学(只抄算法,不抄码)

## Why

SillyTavern World Info 是角色扮演社区十年打磨的知识注入参数学事实标准(AGPL-3.0,**代码不可引入**;算法思想不受版权约束,Go 自写)。四个机制对球球知识域(ADR-0017)是「何时/多强/排谁」的升级——现状检索只回答「有没有」:

- **token 预算优先级选择**:预算耗尽即停,优先级 Constant>高 Order>直接命中>派生——注入从「有没有」到「预算内给谁」;
- **Inclusion Group 互斥消歧**:多条目同击时的显式消歧策略(取最高/加权随机/按命中数);
- **sticky/cooldown 生命周期**:条目「持续 N 轮/冷却 N 轮」——球球版:转会窗类条目「本赛季有效/赛后冷却」;
- **Probability 触发**:低概率条目——主动回合的偶发彩蛋闲聊(不可预测性是 Neuro-sama 研究>90% 观众认定的活着感第一成分)。

门:knowledge-retrieval(波2)先落——检索质量不立,预算学无意义。条目规模小(几十条)时本 change 边际收益低,**门内再评估**。

## What Changes

- 条目 schema 扩展:priority / inclusionGroup / stickyTurns / cooldownTurns / probability(策展台表单随附,默认值=现状行为);
- 检索后处理管道:预算裁剪→互斥消歧→生命周期状态机→概率掷骰(全确定性,Go 自写);
- 红线:**任何作用于外发 prompt 的管道永不触碰比赛事实通道**(ADR-0004);本管道只作用于知识条目注入。

## Non-goals

- SillyTavern 代码级引入(AGPL 传染,宪法级排除);
- 递归扫描(条目链式激活)——条目量级用不上,显式不做;
- 正则改写管道(视图/持久化/prompt 三分离)——记录为表达层备选思路,不实施。

## Success Criteria

- 确定性 eval:预算裁剪次序/互斥消歧/sticky-cooldown 状态机/概率种子可复现;
- 默认值下行为与 knowledge-retrieval 落地后逐字节一致(参数学是可选层);
- 策展台表单扩展测试绿。

## Sequencing

波3 尾,**门=knowledge-retrieval 落地且条目库 ≥50 条**(否则本 change 挂起不实施——ADR-0023 消费先于产能,条目少时预算学无消费者)。
