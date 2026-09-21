# Design: Knowledge RAG

## 条目格式（backend/knowledge/rules/*.yaml）

```yaml
id: rule-offside
topics: ["越位", "offside"]
answer: "简单说，传球一瞬间队友比对方最后一名防守球员更靠近球门线，接下来去拿球就算越位。"
source: "IFAB Laws of the Game 2025-26 Law 11"
confidence: 0.95
effective_at: 2026-07-01
```

条目原文即答案锚（策展时就写成球球口吻的口语）；answer 只能逐字或按模板拼装，不得改写事实句。

## 检索与回答

1. `knowledge_question` 意图命中（词表：越位/犯规/红牌黄牌什么区别/联赛几个队/升降级/积分规则… + router 枚举增行，注册表一处声明）。
2. 检索：topics 关键词命中（contains）∥ embedding 余弦（复用候选 1 通道，200ms 超时），两路各取 top、合并；最高分条目胜出。
3. 回答：确定性拼装 = answer 原文（+可选拼一句「具体哪次判罚你说来我帮你对着看」引导——从短语池取，非 LLM）。`allowRealize=false`，锚点 = answer 首句。
4. 无命中 / confidence < 0.6 → 「这个我还真不敢乱说，等我把功课补上。」——如实不知道。

## ADR-0017 主体

知识域 = 第二事实域：条目 = 知识事实（来源+确信度+生效时间），进 trace（knowledge.answer ToolCall 带条目 id）；条目更新走 git 版本化；与 ADR-0002 的关系是并列事实域而非扩权；ForbiddenClaims 纪律对知识域同样生效（球球不得把知识条目断言成本场事实）。

## 删除测试

删掉 knowledge 包：knowledge_question 退回 unknown 罐头——单点能力，无悬空依赖。
