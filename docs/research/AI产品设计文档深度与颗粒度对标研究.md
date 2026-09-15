# AI 产品设计文档深度与颗粒度对标研究

更新日期：2026-09-03  
研究目的：回答一套 AI 数字人/Agent 产品设计文档在“深度和颗粒度够用”时，应当具备哪些可执行结构，并与本项目 `4.产品设计` 文档集的典型覆盖方式做对照。本文是外部研究和设计审查辅助材料，不是实现状态报告，也不要求产品设计追随任何供应商 API。

## 1. 研究方法与证据边界

- **资料来源**：优先采用 OpenAI、Anthropic、Google、Microsoft、AWS、NIST、GOV.UK 和 Nielsen Norman Group 的官方文档、架构指南或工程文章。
- **检索说明**：此前 Exa 检索已获得 OpenAI、Anthropic、Microsoft、Google、Yellow.ai 等页面的关键段落；本轮 Exa OAuth 刷新令牌已撤销，无法再次调用。报告中的外部结论保留原官方 URL，并对关键页面做了直接访问复核。
- **证据性质**：官方资料可以支持“行业推荐的设计结构、风险控制和运行合同”，不能证明目标用户偏好、长期留存、商业价值或法律合规。
- **项目对照方式**：只阅读 `docs/球球全套资料/4.产品设计` 的文档结构和设计主张，判断覆盖是集中、分散还是需要统一锚点；不把设计领先实现视为缺陷，也不记录代码或交付状态。

## 2. “深度和颗粒度够用”的判断模型

AI 产品设计文档不是功能清单，而是一条可追溯链：

```mermaid
flowchart LR
    A[产品命题与用户任务] --> B[能力/功能设计卡]
    B --> C[用户流程与状态机]
    C --> D[Agent run、工具与数据合同]
    D --> E[错误、回滚、退出与降级]
    E --> F[评测指标、阈值与阻断门]
    F --> G[运营治理与生产变更]
    G --> B
```

一份设计只有同时写清“为什么做、何时发生、发生什么、失败怎么办、如何判断、谁能改”，才足以支撑产品、设计、工程、研究和运营协作。下文把颗粒度分成三个层次：

| 层次 | 最低可执行内容 | 常见不足 |
| --- | --- | --- |
| L1 价值层 | 目标用户、情境、任务、价值、非目标、身份边界 | 只列功能，不说明用户进展和拒绝范围 |
| L2 行为层 | 功能卡、入口/触发、流程、状态、反馈、异常、退出、媒介等价 | 只有正常路径或只有页面描述 |
| L3 运行层 | Agent/tool/run 合同、数据权限、审计、指标阈值、运营门禁、变更回滚 | 把运行规则留给工程临时解释，无法复核设计是否兑现 |

“够用”不等于每个功能都写成技术规格；高影响能力必须达到 L3，低风险展示能力至少达到 L2，所有能力都要能回指 L1 的用户任务。

## 3. 外部一手资料提炼

### 3.1 OpenAI：Agent 定义、运行和可恢复状态

OpenAI Agents SDK 将 Agent 描述为模型、指令、工具，以及可选的 handoff、guardrails、MCP 和结构化输出的组合。官方 Agent definitions 将 `name`、`instructions`、`model`、`tools`、`handoffs`、`outputType`、guardrails 和 hooks 分成可独立审阅的字段；同时强调模型可见的对话历史应与仅供运行时使用的本地上下文分离。

运行时文档把一次 `run` 定义为应用级回合：调用当前 Agent、检查输出、执行工具、处理 handoff，直到最终输出、错误、审批暂停或其他停止条件。Results and state 文档进一步区分最终输出、历史、最后负责的 Agent、响应链 ID，以及审批中断和可恢复状态。OpenAI 的 Agent Evals 指南建议先对单次 trace 做评分，再将样本固化为带版本的数据集和可重复的 eval runs，用于比较提示、路由和工具变更前后的差异。

**可执行结构**：

- Agent manifest：身份、职责、指令、工具、输出 schema、交接目标、护栏、版本；
- Run contract：启动输入、上下文策略、循环步骤、退出原因、最大轮次/时间/预算；
- State contract：暂停、审批、恢复、取消和下一回合如何携带状态；
- Trace contract：模型、工具、handoff、guardrail 的关联事件。

来源：[Agents SDK](https://developers.openai.com/api/docs/guides/agents)（官方 API 文档）、[Agent definitions](https://developers.openai.com/api/docs/guides/agents/define-agents)（官方 API 文档）、[Running agents](https://developers.openai.com/api/docs/guides/agents/running-agents)（官方运行时文档）、[Results and state](https://developers.openai.com/api/docs/guides/agents/results)（官方状态文档）、[Agent Evals](https://developers.openai.com/api/docs/guides/agent-evals)（官方评测文档）。

### 3.2 OpenAI：工具副作用、风险分级与人工审批

OpenAI 的实用指南把工具分为数据、动作和编排三类，要求工具定义标准化、可复用、可测试并易于版本管理；工具风险应按只读/写入、可逆性、权限和财务或社会影响分级。Guardrails and human review 文档将控制点拆为输入、工具行为和输出检查，并把高风险副作用置于可暂停、可审批、可恢复的人工介入流程。

**可执行结构**：

- 工具卡：名称、用途、输入字段/类型、返回 schema、错误 schema、示例、边界；
- 副作用卡：读/写、可逆性、影响对象、授权主体、确认文案、撤销窗口；
- 风险门：低/中/高风险对应自动放行、程序检查或人工审批；
- 审批状态：待审、批准、拒绝、超时、服务不可用及各自用户后果。

来源：[A practical guide to building agents](https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/)（官方工程/产品指南）、[Guardrails and human review](https://developers.openai.com/api/docs/guides/agents/guardrails-approvals)（官方安全与审批文档）。

### 3.3 Anthropic：简单性、工具可用性和循环终止

Anthropic 建议从最简单的提示和单 Agent 方案开始，只有在评测证明复杂编排带来收益时才增加链路。其工程文章把 Agent 描述为“模型使用工具、根据环境反馈循环行动”，强调每一步都要获得 ground truth，并在完成、阻塞或达到最大迭代时停止。工具说明应像给初级开发者写文档一样具体，包含输入格式、示例、边界和防错设计；复杂流程需要在沙箱中充分测试。

**可执行结构**：

- 先写单 Agent 方案和质量基线，再写拆分理由；
- 每个工具都提供可理解的参数和错误边界；
- 中间步骤加入程序化检查或 evaluator；
- 规定最大迭代、失败升级和人工接管；
- 用真实错误样本回归工具选择和参数生成。

来源：[Building effective agents](https://www.anthropic.com/engineering/building-effective-agents)（官方工程文章）。

### 3.4 Microsoft 与 Google：编排模式的选择条件和代价

Microsoft Azure Architecture Center 提供从直接模型调用、单 Agent + 工具到多 Agent 编排的复杂度阶梯，明确提醒多 Agent 会增加协调、延迟、成本和失败模式；单 Agent 也必须设置迭代上限以防无限工具循环。其顺序、并行、群组和交接模式都要求明确状态传递、聚合和失败处理。

Google Cloud 的 Agent 设计模式指南要求在选型前定义任务复杂度、延迟/性能、成本预算和人工参与程度，并将上下文工程列为多 Agent 的核心：不同 Agent 只接收完成任务所需的历史、资料和约束。顺序、并行和循环模式分别有明确的适用条件、聚合策略和终止条件。

**可执行结构**：

- 模式选择卡：任务依赖、并行性、延迟目标、成本预算、人工参与；
- 编排图：节点职责、边的语义（工具调用或所有权交接）、共享状态；
- 聚合规则：冲突结果如何合并、部分失败是否继续；
- 循环规则：退出条件、最大迭代、超时和预算耗尽行为。

来源：[Microsoft AI Agent Orchestration Patterns](https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns)（官方架构指南）、[Google Agent design patterns](https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system)（官方架构指南）。

### 3.5 AWS：从模式蓝图到生产级控制

AWS Prescriptive Guidance 将 Agentic AI patterns 定义为可复用的架构蓝图，覆盖单 Agent、检索增强、语音接口、工作流编排、子 Agent 委派、事件协同、可观测性和控制。其产品领导者、架构师和开发者导向说明：设计文档既要描述单个 Agent 如何感知/推理/行动，也要描述多个 Agent、工具和环境如何组成可扩展、可组合、可审计的工作流。

**可执行结构**：

- 单 Agent pattern card：输入、推理、行动、学习/记忆边界；
- Workflow pattern card：事件、编排器、子 Agent、共享状态、重试和审计；
- 生产控制：可观测事件、权限边界、失败隔离和变更影响。

来源：[Agentic AI patterns and workflows on AWS](https://docs.aws.amazon.com/prescriptive-guidance/latest/agentic-ai-patterns/introduction.html)（AWS 官方 Prescriptive Guidance）。

### 3.6 NIST、GOV.UK 与 NN/g：治理、用户任务和可用性

NIST AI RMF 以 Govern、Map、Measure、Manage 组织风险闭环，要求责任、情境、影响、测量和持续管理可追溯。NIST Generative AI Profile 特别关注幻觉/信息完整性、隐私、偏见、供应链和人机配置风险。

GOV.UK Service Standard 要求理解用户需求、解决完整问题、保证所有人可用、保护隐私、定义成功指标并持续改进；其用户研究指南强调不要把内部解决方案当作用户需求。NN/g 的十项可用性启发式提供状态可见、用户控制、错误预防、错误恢复和帮助文档等交互检查点。

对于带有合成声音或数字人形象的产品，Microsoft 的 Responsible AI 语音指南还要求对声音/头像来源取得充分同意，披露用途、期限和角色身份，并明确生物特征数据的保存与销毁责任。这补充了 Agent manifest 之外的“合成身份资产合同”。

**可执行结构**：

- 风险登记：风险情境、受影响主体、责任人、控制、残余风险；
- 用户任务卡：情境、进展、成功结果、替代路径和停止条件；
- 可用性状态表：等待、处理中、成功、失败、撤销、重试、结束；
- 指标治理：价值、理解、控制、风险四类指标分开，不以互动量抵消安全失败。

来源：[NIST AI RMF](https://www.nist.gov/itl/ai-risk-management-framework)（国家标准与技术研究院框架）、[NIST Generative AI Profile](https://nvlpubs.nist.gov/nistpubs/ai/NIST.AI.600-1.pdf)（官方风险画像）、[GOV.UK Service Standard](https://www.gov.uk/service-manual/service-standard)（官方服务标准）、[NN/g 10 Usability Heuristics](https://www.nngroup.com/articles/ten-usability-heuristics/)（专业启发式指南）。

## 4. 可执行设计结构清单

### 4.1 功能设计卡

每个核心能力建议有唯一编号和版本，字段至少包括：

| 字段 | 要回答的问题 |
| --- | --- |
| 用户任务 | 用户在什么情境下要取得什么进展？ |
| 价值与非目标 | 为什么做？明确不解决什么？ |
| 入口/触发 | 用户动作、状态事件还是有限主动？触发资格是什么？ |
| 前置条件 | 身份、比赛/会话、事实、权限和媒介状态是什么？ |
| 主路径 | 用户动作、系统决策、可见结果按时间顺序是什么？ |
| 状态变化 | 进入、处理中、成功、失败、撤销、过期、结束如何表达？ |
| 控制与退出 | 如何暂停、取消、改主意、删除、返回、求助？ |
| 媒介等价 | 文字、声音、视觉、辅助输入是否能完成同一任务？ |
| 数据副作用 | 读取、写入、保存、共享和删除什么？ |
| 失败与降级 | 数据/模型/供应商/网络失败时保留什么能力？ |
| 评测与门禁 | 正常、反例、控制场景、指标、阈值和阻断条件是什么？ |
| 决策与变更 | 对应哪个产品决策，谁能修改，如何复审/回滚？ |

### 4.2 用户流程与状态机

流程图只表达顺序还不够；状态机必须写清每条转移的触发、守卫条件、用户可见后果和可恢复性。高影响流程至少覆盖：正常、等待、冲突、错误、重试、取消、撤销、超时、权限变化、断线重连和结束。

推荐模板：

```text
状态：名称 / 用户可见说明 / 可执行动作
进入条件：事件、权限、事实资格、控制版本
允许动作：用户动作与 Agent 动作
禁止动作：不可调用的工具、不可呈现的媒介
转移：触发 → 新状态 → 用户可见结果
失败：错误类别、重试上限、回滚/补偿、求助路径
终止：结束原因、待处理计划如何失效、是否可恢复
```

### 4.3 Agent run、工具与输出合同

将 Agent 的自然语言人格与运行合同分开：

- **Manifest**：身份、职责、非目标、触发、模型、工具、输出类型、handoff、guardrails、版本；
- **Run**：输入、上下文、步骤、退出条件、最大轮次/时间/费用、暂停和恢复；
- **Tool**：typed 输入/输出/错误、数据或动作分类、副作用、风险和审批；
- **Response plan**：任务类型、事实资格、媒介资格、长度、禁止方向、失效条件和回退；
- **Trace**：run/agent/tool/handoff/guardrail 关联、耗时、令牌/费用和错误；生产级追踪宜进一步区分 model/tool/handoff/guardrail/custom span。

### 4.4 数据、权限与记忆

每个数据对象都要有来源、目的、作用域、可见主体、读写资格、保存期限、纠正/删除/导出和审计要求。记忆设计不能只写“记住用户”，还应区分本轮状态、会话状态、用户级偏好和共享知识；模型上下文与运行时私有权限分开。Google Agent Engine Memory Bank 的官方文档进一步把记忆生成、事件摄取、配置 profile、读取、修订和 IAM 条件访问拆成独立能力，说明来源、版本和访问权限应进入设计合同。

### 4.5 错误、回滚与降级

错误设计要从用户后果开始，而不是只列 HTTP 状态码：

1. 事实错误/冲突：显示当前可信状态，撤销旧呈现，禁止猜测；
2. 工具失败：说明动作是否执行、是否可重试、是否有重复副作用；
3. Agent 超时/循环：停止并返回部分结果或求助，不无限重试；
4. 审批拒绝/超时：默认不执行，保留可恢复状态；
5. 连接/供应商失败：切换到不扩大权限的文本或静态能力；
6. 配置变更：可比较、可回滚，旧版本正在运行的 run 有明确处理。

### 4.6 评测指标、阈值与门禁

每项高影响能力至少配三类场景：正常任务、高风险反例、用户控制/降级。指标建议分为：

- 价值：任务完成、节省操作、用户主动选择；
- 理解：事实状态复述、身份边界理解、错误原因理解；
- 控制：静音/取消/结束/删除成功率和生效延迟；
- 风险：幻觉、越权、敏感资料泄露、关系压力、错误工具调用；
- 运行：延迟、失败率、重试次数、循环次数、令牌和费用。

阈值应写成“通过/收缩/暂停/复审”规则；互动量、停留时长、好感和转化不能抵消事实、控制、隐私或安全硬门。

### 4.7 运营治理与生产变更

设计文档至少要定义：运营角色和权限、事实发布/更正/撤回、人工接管、事故分级、审计范围、配置审批、灰度/回滚、指标复盘和复审周期。Agent 指令、工具 schema、模型、路由、记忆策略和护栏的变更应有版本号、差异、影响范围、评测结果和回退方案。

## 5. 与本项目 4.产品设计文档的对照

下表是文档结构层面的典型对照，不是实现覆盖率判断。

| 外部“够用”结构 | 本项目对应文档 | 典型完整度判断 | 仍建议统一/补强的颗粒度 |
| --- | --- | --- | --- |
| 产品命题、用户任务、非目标 | 20、21、22 | 强覆盖 | 保持总纲作为唯一价值锚点，功能卡均回指用户任务 |
| 技术边界与媒介等价 | 20A、25、26、34、35 | 强覆盖 | 继续把技术选型写成用户约束和降级合同，不变成供应商说明 |
| 事实资格、冲突、更正、撤销 | 23、24、27、41、42 | 强覆盖 | 统一事实状态枚举、版本和跨媒介失效规则 |
| Agent identity/scope/trigger | 28、31、33A | 较强但分散 | 以 33A 增加可审阅的 manifest 字段表，链接人格、主动性和工具边界 |
| Run 生命周期与终止 | 26、33A、42 | 强覆盖 | 把最大轮次/时间/费用、暂停恢复和终止原因集中成 run 合同 |
| Typed tools、副作用与审批 | 36、39、42、44 | 部分到较强 | 形成统一工具卡：输入/输出/错误/风险/授权/撤销/审计 |
| Handoff 与编排模式 | 33A、39、46 | 较强 | 明确每个交接的上下文范围、所有权、失败回退和用户告知 |
| Memory 与数据权利 | 29、30、37、38、41 | 强覆盖 | 增加来源可信度、读写资格、版本和异步/缓存删除语义的统一字段 |
| Input/tool/output guardrails | 28、36、42、45 | 较强 | 用同一张边界表标明检查点、默认拒绝、人工接管和审批超时 |
| 用户流程、状态机、错误降级 | 21–27、31、35、40、42 | 强覆盖 | 每张核心场景卡补齐异常、回滚/补偿、出口和媒介等价验收 |
| 评测指标与阻断阈值 | 43、44、45 | 强覆盖 | 让指标与功能卡/决策编号双向回指，区分价值、理解、控制、风险 |
| 运营治理与生产变更 | 36、39、44、45、46 | 部分到较强 | 补充配置版本、变更 diff、灰度/回滚、事故复盘和责任链模板 |
| 可观测性与成本延迟 | 20A、33A、42、44、45、46 | 较强但需收口 | 统一 run trace 字段（model/tool/handoff/guardrail/custom span）、token/费用、延迟、失败和循环预算，并定义数据集版本与变更前后对比 |

### 5.2 生产版本能力拓展（对应审查标准 H）

审查标准已增加 H「生产版本能力拓展」。这一维度不是把生产能力当成交付状态，而是检查目标态设计是否为自动事实发布、提醒、长期连续性、受控工具、外部动作和语音/舞台主体验逐项写清用户价值、触发、授权、事实资格、责任、副作用、失败/撤回/退出、评测和回滚。

对照现有文档可见：

- `33A-数字球友Agent运行时与决策编排设计.md` 的生产段已覆盖单 Agent run、规划/工具/暂停/恢复/取消/终止、工具副作用、超时、幂等、撤销、授权、护栏、预算、审批和 Trace；
- `20A-技术约束与选型原则.md` 已把生产运行的延迟、循环、费用、工具预算、供应商灰度/熔断/回退和字段级许可、有效期、删除屏障写成产品约束；
- `42-API与实时协议设计.md` 已给出 tool 请求/完成/撤销、审批、授权/确认/责任/副作用、终态优先和回滚语义；
- `45-产品评测体系设计.md` 已把工具动作、预算、幂等、撤销、人工接管及 run/工具/事实/控制/终止关联纳入生产评测卡；
- `46-系统架构图集.md` 已覆盖 input/tool/output/human 四层护栏、run 预算/超时/循环上限、Trace、污染和审批超时演练。

因此，H 维度的主要结论不是“缺少生产设计”，而是“已有目标态覆盖，仍需把合同字段、枚举和评测阈值集中收口”。多 Agent 编排不应被强行加入：如果产品明确保持单 Agent 边界，外部多 Agent 模式属于不适用的对照项，而不是缺陷。

### 5.3 颗粒度层面的残余差距（不等同于原则缺陷）

- **Manifest 仍跨篇分布**：33A 是跨篇总契约，但身份、触发、人格、主动性、工具、输出和所有权仍分别位于 20、28、31、33A、42、46；外部 Agent 文档更常用一张字段表作为审阅入口。
- **工具合同还可逐工具落表**：现有文档已定义工具类别、请求/完成/撤销、授权、确认、责任、副作用和预算，但与外部“typed tool”基线相比，仍可为每个工具补齐参数 schema、返回 schema、错误 schema、边界示例和统一终止原因枚举。
- **模型上下文与运行时私有上下文需显式分栏**：现有文档已有上下文最小化和字段级许可原则；可再明确哪些字段进入模型可见历史，哪些只保留在运行时依赖中，避免身份/权限对象意外暴露给模型。
- **Trace 与评测基线需可比较**：45、46 已有 run 关联、硬门和演练；外部 Agent Evals/observability 基线还要求 trace grader、数据集版本、model/tool/handoff/guardrail span，以及变更前后 eval run 对比。建议将这些作为 G/H 的执行细化项，而非删除或降低目标态设计。
- **预算与重试策略需字段化**：20A、33A、46 已提出循环、延迟、费用、工具预算和降级原则；下一步可把最大轮次、超时、重试次数、费用上限和部分失败策略做成统一字段与阈值。

### 5.4 按审查标准 A–H 的快速结论

| 维度 | 对照结论 | 说明 |
| --- | --- | --- |
| A 产品命题与用户价值 | 通过 | 20–22 已把用户任务、价值、非目标和观看进展作为总纲。 |
| B 术语、模式与一致性 | 部分通过 | 核心术语和事实枚举较完整；唯一 Agent manifest、工具字段和终止枚举仍需集中锚定。 |
| C 旅程、交互与状态 | 目标态已覆盖 | 多篇状态机和异常路径较完整；统一 run 字段、重试/终止枚举可提升可执行性。 |
| D 身份、关系与内容安全 | 通过 | 28、31、36 形成身份透明、关系边界、输入安全和人工支持约束。 |
| E 数据、隐私与权利 | 强通过 | 29、30、37、38 覆盖作用域、删除和用户权利；记忆来源、读写主体和污染反例仍可汇总。 |
| F 技术约束、媒介等价与降级 | 通过 | 20A、25、26、34、35、42 已明确文字基线、取消、回退、预算和等价路径。 |
| G 评测、指标与决策治理 | 部分通过 | 45、46 有场景、硬门、Trace 和演练；trace grading、数据集版本、定量基线和变更对比待补。 |
| H 生产版本能力拓展 | 目标态已覆盖 | 20A、33A、42、45、46 已覆盖生产 run、受控工具、审批、回滚和运行门；应继续做合同字段收口。 |

### 5.1 对照结论

1. **总体深度已超过“功能说明书”**：现有文档已形成从命题、旅程、事实、语音、人格、关系、记忆、隐私、运营到协议和评测的连续设计系统；状态机、控制优先级、事实资格、退出和媒介降级等颗粒度达到 Agent 产品可执行设计的较高水平。
2. **生产拓展已有目标态覆盖**：20A、33A、42、45、46 已写入生产运行、工具副作用、审批、预算、Trace、回滚和演练等结构；这些设计可以领先于交付，不应因为工程覆盖问题删改。
3. **最大结构性差距不是缺少专题，而是合同分散**：外部 Agent 文档通常用 manifest、tool schema、run state、trace 等少数标准表把执行合同集中起来；本项目相同内容分布在多个专题中，阅读者需要跨篇拼接。
4. **优先统一三类锚点**：Agent manifest、工具/动作卡、run/trace 合同。它们属于设计沉淀，不等于把实现状态写入产品正文。
5. **评测结构已经成熟，阈值治理仍可更统一**：45 已有场景卡、硬门/体验门和收缩/暂停规则；下一步重点是让每个高影响能力的指标、阈值、预算和决策编号可双向回指，并补齐 trace grader、数据集版本和变更前后比较。
6. **技术选型位置不构成主要问题**：20A 已明确放在产品设计前面并以用户控制、事实、降级、隐私和可观测性约束技术；后续只需保持“先产品约束、后具体供应商”的叙述顺序。

## 6. 建议的统一模板（可作为后续设计沉淀）

不要求重写全部文档，可为每个高影响能力附一张统一卡片：

```text
能力编号 / 版本 / 决策编号
用户任务 / 价值 / 非目标
触发与前置状态
Agent manifest（身份、范围、工具、输出、交接）
主流程与状态机
数据与权限（来源、作用域、读写、保存、删除）
正常结果 / 错误 / 冲突 / 取消 / 回滚 / 降级
媒介等价（文字、声音、视觉、辅助输入）
风险与 guardrails（检查点、审批、默认拒绝）
评测场景（正常、反例、控制）
指标 / 阈值 / 阻断 / 收缩 / 复审
trace、成本、延迟与预算
运营责任 / 变更 diff / 灰度 / 回滚
```

这张卡的作用是把跨文档设计压缩成可审阅入口；详细原则、旅程、事实模型和界面规格仍保留在各自源文档中。

## 7. 外部对标的使用边界

- 不把 OpenAI Agents SDK、AWS 服务或任何平台字段直接当作本项目产品需求；应抽取身份、边界、状态、权限和责任等设计问题。
- 不以官方文档证明用户喜欢某种人格、主动性、记忆或语音节奏；这些仍需目标用户研究和受控评测。
- 不因某一结构在当前工程中尚未出现，就删减设计目标；只需确保设计主张有边界、验收和收缩路径。
- 不把“文档结构完整”表述为“产品已经安全、有效或上线”；它只说明设计更容易被协作、验证和治理。

## 8. 来源清单（官方/一手资料）

1. OpenAI — A practical guide to building agents：<https://openai.com/business/guides-and-resources/a-practical-guide-to-building-ai-agents/>（官方工程/产品指南）
2. OpenAI API — Agents SDK：<https://developers.openai.com/api/docs/guides/agents>（官方 API 文档）
3. OpenAI API — Agent definitions：<https://developers.openai.com/api/docs/guides/agents/define-agents>（官方 API 文档）
4. OpenAI API — Running agents：<https://developers.openai.com/api/docs/guides/agents/running-agents>（官方运行时文档）
5. OpenAI API — Results and state：<https://developers.openai.com/api/docs/guides/agents/results>（官方状态文档）
6. OpenAI API — Orchestration and handoffs：<https://developers.openai.com/api/docs/guides/agents/orchestration>（官方编排文档）
7. OpenAI API — Guardrails and human review：<https://developers.openai.com/api/docs/guides/agents/guardrails-approvals>（官方安全与审批文档）
8. OpenAI API — Agent Evals：<https://developers.openai.com/api/docs/guides/agent-evals>（官方评测文档）
9. OpenAI API — Integrations and observability：<https://developers.openai.com/api/docs/guides/agents/integrations-observability>（官方可观测性文档）
10. Anthropic — Building Effective Agents：<https://www.anthropic.com/engineering/building-effective-agents>（官方工程文章）
11. Microsoft Azure Architecture Center — AI Agent Orchestration Patterns：<https://learn.microsoft.com/en-us/azure/architecture/ai-ml/guide/ai-agent-design-patterns>（官方架构指南）
12. Google Cloud Architecture Center — Choose a design pattern for your agentic AI system：<https://docs.cloud.google.com/architecture/choose-design-pattern-agentic-ai-system>（官方架构指南）
13. AWS Prescriptive Guidance — Agentic AI patterns and workflows on AWS：<https://docs.aws.amazon.com/prescriptive-guidance/latest/agentic-ai-patterns/introduction.html>（官方架构/实施指南）
14. NIST — AI Risk Management Framework：<https://www.nist.gov/itl/ai-risk-management-framework>（国家标准与技术研究院框架）
15. NIST — Generative AI Profile：<https://nvlpubs.nist.gov/nistpubs/ai/NIST.AI.600-1.pdf>（国家标准与技术研究院风险画像）
16. Google Cloud — Agent Engine Memory Bank overview：<https://cloud.google.com/agent-builder/agent-engine/memory-bank/overview>（官方记忆管理文档）
17. Microsoft Responsible AI — Speech voice talent disclosure：<https://learn.microsoft.com/en-us/azure/ai-foundry/responsible-ai/speech-service/text-to-speech/disclosure-voice-talent>（官方合成声音透明与同意指南）
18. Microsoft Azure AI Speech — Text-to-speech avatar overview：<https://learn.microsoft.com/en-us/azure/ai-services/speech-service/text-to-speech-avatar/what-is-text-to-speech-avatar>（官方数字人能力文档）
19. GOV.UK — Service Standard：<https://www.gov.uk/service-manual/service-standard>（政府服务设计标准）
20. Nielsen Norman Group — 10 Usability Heuristics：<https://www.nngroup.com/articles/ten-usability-heuristics/>（专业可用性启发式）
