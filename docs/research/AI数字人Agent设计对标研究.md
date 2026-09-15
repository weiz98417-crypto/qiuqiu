# AI 数字人 Agent 设计对标研究

更新日期：2026-09-02  
研究目的：整理公开的一手 Agent 设计规范，形成可用于产品设计审查的外部能力基线。本文讨论“应该如何设计和审查 Agent”，不判断任何具体项目是否已经实现，也不把外部产品的实现方式当作本项目的既定方案。

## 1. 研究范围与方法

- **研究对象**：面向对话、语音或多模态交互的 AI Agent，以及支撑其运行的工具、记忆、编排、安全和评测设计。
- **资料优先级**：OpenAI、Anthropic、Microsoft、Google 的官方文档和官方工程文章；行业平台官方文档仅用于补充产品化表达。
- **证据性质**：官方规范/架构指南说明推荐做法或平台能力，不等同于用户偏好、业务成功或法律合规证明。
- **使用方式**：把下文能力项作为外部对标基线，逐项检查产品设计是否有清晰主张、边界、状态、控制和评测；不因某项尚未实现就自动判定设计错误。

## 2. 外部 Agent 设计的共同模型

### 2.1 Agent manifest：身份、职责与运行合同

高质量 Agent 文档通常把一个 Agent 描述为可审阅的配置合同，而不只是人格提示词。至少应能回答：

| 合同字段 | 设计问题 | 外部依据 |
| --- | --- | --- |
| 身份（name/identity） | 它是谁，如何向用户和其他 Agent 表明身份？ | OpenAI Agents SDK 将 `name` 作为追踪和工具/交接界面的可读身份；Google 单 Agent 模式要求系统提示定义任务、角色和操作。 |
| 目标与范围（instructions/scope） | 它负责什么、不负责什么，何时拒绝或转交？ | OpenAI Agent definitions 要求 instructions 描述工作、约束和风格；Yellow.ai 将身份、范围和规则分开配置。 |
| 触发（trigger） | 什么输入或事件使它接管？触发描述与行为规则是否分离？ | Yellow.ai 把 Trigger 定义为路由信号，把 Agent instructions 定义为接管后的行为规则。 |
| 能力（tools/output） | 可调用哪些工具，输出是否需要结构化合同？ | OpenAI Agent definitions 将 tools、outputType、guardrails、handoffs 列为独立配置面。 |
| 所有权（ownership） | 谁对最终回复、工具副作用和失败负责？ | OpenAI 将 handoff（交接最终回复所有权）与 agents-as-tools（管理者保留所有权）明确区分。 |
| 版本与变更 | 身份、指令、工具、策略和输出合同如何版本化、审阅和回滚？ | OpenAI 实用指南强调标准化、可复用、易发现、易测试和简化版本管理的工具定义；平台文档普遍把 Agent 配置拆成可独立审阅字段。 |

**可复用基线**：产品设计应有一页或一个章节能稳定描述每个 Agent 的身份、目标、非目标、触发、工具、输出、交接和版本边界；“人格设定”不能替代这些字段。

### 2.2 Typed tools：工具定义、风险和审批

外部资料把工具视为 Agent 的“行动接口”，并强调工具合同的可读性和副作用控制：

1. **标准化与类型化**：工具应有明确名称、参数类型、必填条件、返回结构、错误结构、示例和边界；结构化输出用于让下游代码可靠消费，而非从自然语言猜测字段。来源：[OpenAI 实用指南](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)、[OpenAI Agent definitions](https://developers.openai.com/api/docs/guides/agents/define-agents)、[Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)。
2. **工具分类**：数据读取、动作执行和编排调用是三类常见工具；动作工具应明确是否产生外部副作用。来源：[OpenAI 实用指南](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)。
3. **风险分级**：按只读/写入、可逆性、权限范围和财务或社会影响分为低、中、高风险，并把等级连接到检查、审批或人工接管。来源：[OpenAI 实用指南](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)。
4. **边界校验**：工具调用前校验调用身份、目标、参数和授权范围；高风险动作在副作用边界暂停，审批超时或不可用时应默认拒绝。来源：[OpenAI Guardrails and human review](https://developers.openai.com/api/docs/guides/agents/guardrails-approvals)。
5. **防错设计**：工具参数应尽量让错误难以发生，并用真实示例和边界案例反复测试模型的工具选择。来源：[Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)。

**可复用基线**：产品设计至少应区分“建议/查询”和“代表用户执行动作”，为后者规定确认、撤销、失败和责任归属；只写“Agent 可以调用某服务”不足以构成可审查的工具设计。

### 2.3 Run lifecycle：一次运行而非只有一段会话

官方指南普遍把 Agent 的最小控制单位定义为 run：

- **启动**：接收用户指令或事件，建立本次运行上下文和权限。
- **循环**：模型根据当前状态选择输出或工具；工具结果回到模型，直到满足退出条件。
- **退出条件**：最终结构化输出、无工具调用的直接回复、明确错误、用户取消、达到最大轮次/时间/预算，或转交给人/其他 Agent。来源：[OpenAI 实用指南](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)、[Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)、[Google Agent design patterns](https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system)。
- **暂停与恢复**：需要审批或补充信息时保存可恢复状态，决定返回后继续同一 run，而不是隐式开启新会话。来源：[OpenAI Guardrails and human review](https://developers.openai.com/api/docs/guides/agents/guardrails-approvals)。
- **失败处理**：工具失败、模型无法完成、上下文超限或路由失败时，给出可理解状态、重试上限、降级结果和求助入口。Microsoft 明确提醒单 Agent 需要迭代上限以防无限工具循环。来源：[Microsoft AI Agent Orchestration Patterns](https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns)。

**可复用基线**：设计应分别描述“会话生命周期”和“单次 Agent run 生命周期”，并给每个终止/暂停原因定义用户可见后果、可恢复性和成本上限。

### 2.4 Orchestration 与 handoff：模式、上下文和责任

多 Agent 设计的关键不是数量，而是所有权和上下文合同：

| 模式 | 所有权 | 适用情境 | 主要控制点 |
| --- | --- | --- | --- |
| 单 Agent + 工具 | 一个 Agent 保留回复所有权 | 领域单一、工具数量可控 | 工具边界、循环上限、失败回退 |
| Manager / agents-as-tools | 管理者保留最终回复，专家返回有界结果 | 需要综合多个专家结果 | 专家输出 schema、上下文最小化、成本预算 |
| Handoff | 控制和下一次回复所有权移交专家 | 不同职责/策略需要独立接管 | 触发描述、移交上下文、责任提示、回退路径 |
| Sequential | 预定义线性阶段传递状态 | 阶段依赖明确、不可并行 | 每阶段输入输出、失败阻断 |
| Concurrent / fan-out-fan-in | 并行专家独立处理，再聚合 | 需要多视角或降低等待 | 聚合冲突策略、并发成本、部分失败 |
| Loop | 重复专家序列直到状态满足 | 迭代改进或自校正 | 明确退出条件和最大迭代，防无限循环 |

依据：[OpenAI Orchestration and handoffs](https://developers.openai.com/api/docs/guides/agents/orchestration)、[Microsoft AI Agent Orchestration Patterns](https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns)、[Google Agent design patterns](https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system)。

外部文档还强调 context engineering：为每个 Agent 只传递完成其任务所需的历史、资料和约束，区分模型可见的对话历史与运行时私有依赖。来源：[OpenAI Agent definitions](https://developers.openai.com/api/docs/guides/agents/define-agents)、[Google Agent design patterns](https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system)。

**可复用基线**：每个交接点至少应有“为什么触发、传什么、谁负责下一次回复、失败退回谁、用户是否被告知”的设计说明；不能只画箭头而不写上下文和责任。

### 2.5 Memory：范围、来源、读写资格和生命周期

公开 Agent 文档通常把 memory 当作受控状态，而不是笼统的“记住用户”：

- **范围**：本轮临时状态、会话状态、用户级长期偏好、租户/角色级共享知识应分开；共享状态要说明可见主体。
- **来源与可信度**：记忆来自用户明确提供、系统观察、工具返回还是模型推断；不同来源不能默认拥有相同可信度。
- **读写资格**：哪些 Agent/工具可以读取或写入，是否需要用户同意、确认或敏感信息过滤。
- **生命周期**：保存期限、版本、纠正、删除、撤回后的停止读取，以及缓存/重试/异步任务的处理。
- **上下文最小化**：只有模型需要的事实进入模型上下文；仅供运行时使用的身份、权限和数据库客户端保留在本地上下文。来源：[OpenAI Agent definitions](https://developers.openai.com/api/docs/guides/agents/define-agents)、[Google Agent design patterns](https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system)。

**可复用基线**：设计文档应提供 memory schema 或字段表，至少包含 key、来源、作用域、可见性、写入条件、过期/删除和审计要求；“长期陪伴”不能替代资料权利说明。

### 2.6 Guardrails 与 human-in-the-loop：四个边界

OpenAI 将控制点明确分为输入、工具和输出 guardrails，并以 human review 处理需要授权的副作用：

1. **Input**：主 Agent 运行前拦截不允许的请求、越权资料或不适用场景。
2. **Tool call / result**：校验参数、调用身份、结果格式和副作用；工具结果也要防提示注入和恶意内容。
3. **Output**：最终响应发送前做事实、隐私、格式、品牌和安全检查，可拒绝或重写。
4. **Human review**：高风险、不可逆或模糊动作暂停，记录待审项、审批人、决定和恢复状态；拒绝、超时和审批服务不可用均有默认路径。

来源：[OpenAI Guardrails and human review](https://developers.openai.com/api/docs/guides/agents/guardrails-approvals)、[OpenAI 实用指南](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)、[NIST AI RMF](https://www.nist.gov/itl/ai-risk-management-framework)。

Anthropic 还建议在中间步骤设置程序化检查、在沙箱中进行充分测试，并允许 Agent 在遇到阻塞时回到人类判断。来源：[Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)。

**可复用基线**：安全设计不能只有“禁止回答”的内容政策；必须说明检查发生在哪个边界、失败后是否停止、用户如何理解和恢复，以及何时升级人工。

### 2.7 Observability、评测和治理

成熟 Agent 设计把可观测性和评测当作运行合同的一部分：

- **Trace**：记录 run、Agent、模型调用、工具调用、guardrail、handoff 的关联 ID、输入/输出摘要、耗时、令牌/费用和错误；支持按一次 run 回放责任链。来源：[OpenAI Agents SDK](https://developers.openai.com/api/docs/guides/agents)、[OpenAI Agents SDK Python](https://openai.github.io/openai-agents-python/agents/)。
- **Lifecycle hooks**：在 Agent start/end、LLM start/end、tool start/end、handoff 等节点记录事件或执行策略。来源：[OpenAI Agents SDK Python](https://openai.github.io/openai-agents-python/agents/)。
- **评测维度**：任务完成率、工具选择/参数正确率、事实准确性、拒答正确率、升级/接管率、延迟、令牌与费用、失败和循环次数；价值指标不能替代风险指标。来源：[OpenAI Agents SDK](https://developers.openai.com/api/docs/guides/agents)、[OpenAI Running agents](https://developers.openai.com/api/docs/guides/agents/running-agents)、[NIST AI RMF](https://www.nist.gov/itl/ai-risk-management-framework)。
- **反例与红队**：覆盖越权、提示注入、敏感资料泄露、错误工具、冲突事实、重复输出、断线、审批超时和无限循环；上线后持续监测并回归测试。来源：[NIST Generative AI Profile](https://nvlpubs.nist.gov/nistpubs/ai/NIST.AI.600-1.pdf)、[Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)。
- **变更治理**：Agent 指令、工具 schema、模型、路由和 guardrail 的每次变更可审阅、可比较、可回滚，并关联评测结果和决策记录。工具标准化和版本管理的建议见：[OpenAI 实用指南](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)。

**可复用基线**：设计文档不必承诺具体监控产品，但要定义可观察事件、关键指标、阻断阈值、复审周期和变更责任人。

### 2.8 成本、延迟与复杂度预算

外部架构指南都把成本、延迟和复杂度视为同一设计决策的约束：

- 先用最简单的单 Agent 或非 Agent 方案建立质量基线，再证明多 Agent 的收益；每新增 Agent 都会增加提示、追踪、审批面、失败模式和推理成本。来源：[OpenAI Orchestration](https://developers.openai.com/api/docs/guides/agents/orchestration)、[Microsoft AI Agent Orchestration Patterns](https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns)、[Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents)。
- 复杂任务可按阶段使用不同模型：检索/分类使用更快更便宜的模型，关键判断使用更强模型；先用强模型建立质量基线，再用评测验证降本。来源：[OpenAI 实用指南](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)。
- 并行编排可降低墙钟延迟，但会提高并发 token 和聚合成本；串行编排可控但可能积累延迟；循环必须有最大轮次/预算。来源：[Google Agent design patterns](https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system)、[Microsoft AI Agent Orchestration Patterns](https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns)。

**可复用基线**：每个高频任务应有可接受延迟、最大步骤/轮次、费用预算和降级策略；“更聪明”不能成为无限调用的理由。

## 3. 与标准审查维度的映射

下表把外部 Agent 基线映射到产品设计审查的 A–G 维度，便于形成问题清单；它是审查提示，不是对任何文档的预先判定。

| 外部能力项 | 主要审查维度 | 应检查的设计证据 |
| --- | --- | --- |
| Agent identity/scope/trigger/manifest | A、B、D | 身份、目标/非目标、触发、角色边界、版本和正式术语 |
| Typed tools 与风险审批 | C、D、E、F、G | 工具 schema、副作用、风险等级、确认/撤销、审批和失败 |
| Run lifecycle 与 termination | C、F、G | 启动、循环、暂停、终止、最大轮次/时间/费用、恢复和求助 |
| Orchestration/handoff | B、C、D、F | 模式选择、上下文范围、责任归属、回退和用户告知 |
| Memory | D、E、F | 来源、作用域、读写资格、生命周期、删除/撤回和最小上下文 |
| Guardrails/HITL | C、D、E、G | input/tool/result/output 四个边界、人工接管、默认拒绝 |
| Observability/evaluation | G、C、D | trace、事件、指标、反例、红队、发布/收缩/暂停门槛 |
| Version/cost/latency | B、F、G | 配置版本、变更 diff、模型分层、预算、延迟和复杂度取舍 |

## 4. 对 AI 数字人设计文档的最低结构建议

一套可审阅的 AI 数字人 Agent 设计资料，建议至少包含以下章节（可分散在多份文档，但导航和唯一锚点要明确）：

1. 产品命题、目标用户、使用情境、非目标和身份边界。
2. Agent manifest：身份、职责、触发、指令、模型、工具、输出、handoff、版本。
3. 交互与媒介：文字/语音/视觉的等价路径，监听、思考、生成、播放、打断、静音、断线和恢复。
4. Run 与状态机：生命周期、退出条件、暂停/恢复、重试、预算和求助。
5. 工具与动作：typed schema、数据/动作/编排分类、副作用、风险等级、确认和撤销。
6. 编排与上下文：单 Agent、manager、handoff、顺序/并行/循环模式，以及上下文过滤和责任归属。
7. Memory 与资料权利：来源、作用域、读写、保存、纠正、删除、导出、撤回和审计。
8. Guardrails 与人工接管：输入、工具调用/结果、输出、审批、拒绝、升级和事件响应。
9. 事实与内容治理：事实资格、来源、冲突/不确定表达、人格和关系边界、敏感场景。
10. 评测与发布治理：任务成功、理解、控制、风险、延迟、成本、反例、红队、阻断条件和变更复审。

## 5. 外部对标的使用边界

- 外部平台的字段名和 API 不是产品设计必须采用的界面；应抽取其背后的控制问题，再用本项目的用户语言表达。
- 官方文档能证明“行业通常如何定义 Agent 能力和风险”，不能证明目标用户偏好、留存或长期关系价值。
- 多 Agent、长期记忆和高自治会增加延迟、成本、失败模式和隐私暴露面；只有在用户任务收益可测量时才值得引入。
- 设计可以领先当前工程覆盖，但领先项仍需满足身份透明、用户控制、事实与隐私边界，并具备可验证的验收和收缩路径。

## 6. 来源清单（均为官方一手资料）

1. OpenAI — A practical guide to building agents：<https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/>（官方工程/产品指南）
2. OpenAI API — Agents SDK：<https://developers.openai.com/api/docs/guides/agents>（官方 API 文档）
3. OpenAI API — Agent definitions：<https://developers.openai.com/api/docs/guides/agents/define-agents>（官方 API 文档）
4. OpenAI API — Orchestration and handoffs：<https://developers.openai.com/api/docs/guides/agents/orchestration>（官方 API 文档）
5. OpenAI API — Guardrails and human review：<https://developers.openai.com/api/docs/guides/agents/guardrails-approvals>（官方 API 文档）
6. OpenAI Agents SDK Python — Agents and lifecycle hooks：<https://openai.github.io/openai-agents-python/agents/>（官方 SDK 文档）
7. OpenAI API — Running agents：<https://developers.openai.com/api/docs/guides/agents/running-agents>（官方运行时文档）
8. OpenAI API — Results and state：<https://developers.openai.com/api/docs/guides/agents/results>（官方状态与可恢复运行文档）
9. Anthropic — Building Effective Agents：<https://www.anthropic.com/engineering/building-effective-agents>（官方工程文章）
10. Microsoft Azure Architecture Center — AI Agent Orchestration Patterns：<https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns>（官方架构指南）
11. Google Cloud Architecture Center — Choose a design pattern for your agentic AI system：<https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system>（官方架构指南）
12. Yellow.ai — Single agents / Conversational agents：<https://docs.yellow.ai/docs/nexus/build/agents/conversational-agents>（行业平台官方产品文档，补充 Trigger、Lifecycle、Memory 的产品化表达）
13. NIST — AI Risk Management Framework：<https://www.nist.gov/itl/ai-risk-management-framework>（美国国家标准与技术研究院风险治理框架）
14. NIST — Generative AI Profile：<https://nvlpubs.nist.gov/nistpubs/ai/NIST.AI.600-1.pdf>（美国国家标准与技术研究院生成式 AI 风险画像）
