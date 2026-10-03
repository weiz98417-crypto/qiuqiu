# 知识检索加深:双路融合与运营可调

## Why

Knowledge Entry 是 verbatim 承诺域(ADR-0017):漏检等于假装「不知道」,直接违背知识域的存在意义。现状检索质量被三处设计压制(走查证据):

1. **关键词一票优先**:任一 topic 词 contains 命中即返回,向量路无出场机会(knowledge.go:218-238)——「越位算不算进球」这类多 topic 交叉查询按 score 取最高,并列取先声明条目;
2. **阈值硬编码**:向量 cos≥0.55(knowledge.go:261)、confidence≥0.6(:19),无运营调节点;
3. **检索面窄+缓存脆**:只对 Topics 检索不含 Answer/Quote(:289);topicVecs 按条目下标缓存、Reload 全清重嵌(:115-118)——条目增长后每次编辑的代价线性放大。

memory 包已有验证过的三腿融合先例(queue.go:406-491)——同一仓库内两代技术并存,这是收编不是发明。

## What Changes

- **双路评分融合**:关键词路与 bge-m3 向量路并行评分、归一化融合取 Top-K(照 memory 三腿形态);并列消歧显式化(次序规则落注释与测试)。
- **阈值 config 化**:cos/confidence 阈值进 config,运营侧可调(知识策展台后续暴露,本 change 先进 config)。
- **检索面扩展**:条目 Answer/Quote 参与向量检索(检索时嵌入、条目维度不变)。
- **缓存改内容键**:topicVecs 按内容 hash 缓存,Reload 只重嵌变化条目。

## Non-goals

- 知识域条目结构/DB schema 变化(检索层 only)。
- 运营台检索调参 UI(阈值进 config 即可,策展台面板后置)。
- World Info 参数学(预算优先级/互斥组/sticky-cooldown——knowledge-worldinfo 独立 change,本 change 是它的前置)。
- 重排序模型/交叉编码器(bge-m3 单路够用,引入前先看融合后命中率)。

## Success Criteria

- 确定性 eval:检索排序套件——多 topic 交叉/向量优于关键词的查询/阈值边界/并列消歧/Reload 后缓存命中(DB store 模式进 eval,现夹具走 YAML 直读不覆盖本层);
- 条目 50+ 时 Reload 重嵌数量只随变化条目数;
- 策展台既有测试(Knowledge.test 10 例)与后端 knowledge 套件全绿——检索行为变化只发生在「原本就检不到/检错」的查询上,新增命中是净收益。
