---
lesson: 4
title: 表达层：438 字节提示词的分工
trace_position: 佩德里进球 trace 第 5 站（共 7 站）：事实已锁定"法比安助攻"，Director 已决定"报事实+庆祝"，本站把已批准的决定变成自然中文台词
depends_on: [27-dialogue-expression, 28-persona-constitution]
---
# 第 4 讲 · 表达层：438 字节提示词的分工

## 这一站在 trace 上

第 24 分钟佩德里进球后，用户问"刚才是谁助攻的？"。前三站已经把事实锁定为"法比安助攻、亚马尔策动"，策略表选好了沟通动作和硬约束（≤2 句 80 字、问句是否允许）。本站出场的是两个提示词加一个验证器：语言实现层（LLM）只负责把决定说顺，不负责决定。它拿到的是任务书，不是白纸；越界就被打回确定性底稿。

```
Director 决定(动作+底稿+锚点) ──► LLMReplyRealizer.Realize ──► validateRealizedText
        ▲                                                        │
        └────────── 报错/超时/越界：打回 reliable 底稿 ◄──────────┘
```

## 证据锚点

- `backend/internal/companion/realize.go:35-42` — realizer 系统提示词：只负责说，不能改动作/阶段/事实/边界
- `backend/prompts/v1.0/system.txt:1-4` — 语音助手底层提示词，实测 438 字节 4 行（`wc -c` 可复验）
- `backend/internal/companion/realize.go:53-56` — 可靠底稿 / 必须保留（RequiredAnchors）/ 禁止新增（ForbiddenClaims）注入 user 消息
- `backend/internal/relationship/types.go:368-381` — ContentPolicy：锚点、禁语、句数/字数/问句上限
- `backend/internal/companion/agent.go:816-823` — `allowRealize`/`deterministicReason` 闸门变量与 `claim_policy` 分支
- `backend/internal/companion/agent.go:1045-1050` — fact_language_policy：含比赛事实语言却检索不到锚点 → 禁用 LLM
- `backend/internal/companion/agent.go:2201-2207` — containsMatchFactLanguage 关键词表（助攻/比分/VAR/红牌…）
- `backend/internal/companion/agent.go:2572-2584` — realizer 报错或验证不过 → 回退确定性底稿
- `backend/internal/companion/agent.go:2684-2729` — validateRealizedText 的全部硬校验

## 代码走读

### 1. 底层提示词：438 字节管"是谁"（backend/prompts/v1.0/system.txt:1-4）

```text
你是球球，陪用户看球的AI语音助手。像朋友一样聊天，不是解说员/教练/数据员。
懂足球但不用战术术语。性格开朗，偶尔吐槽但不攻击任何人。
风格：每次≤2句，自然口语，表达情绪+观察，变换说法。可用"哇""哎""好球！"开头。
底线：不攻击球队/球员/裁判，不谈政治/宗教/种族，不鼓励赌博，不用脏话，不做绝对预测。
```

这份文件由 `backend/cmd/server/main.go:187` 的 `promptMgr.LoadSystem` 加载，服务导播主动线等 pipeline 场景。438 字节干的事只有人设和底线，不承诺任何事实能力。注意它和 realizer 提示词是两份文件、两个岗位，别混为一谈。

### 2. realizer 提示词：管"怎么说话"（backend/internal/companion/realize.go:35-42）

```go
			Content: strings.TrimSpace(`你是球球的语言实现层，只负责把已经决定好的沟通动作说成自然中文。
球球是一起看足球的数字球友，不是客服、解说员、治疗师或恋爱伴侣。
不能改变沟通动作、关系阶段、事实、不确定性、边界、调侃许可或是否追问。
不要使用服务腔，不复述用户原话，不固定采用“接情绪+分析+反问”，不要每轮都提问。
禁止恋爱化、排他化、依赖性表达，禁止编造人类生活经历，禁止提及模型、后台、导播台或内部记录。
只在当前话题确实相关时自然带过给出的共同上下文，不炫耀记忆能力，不解释来源。
沟通动作包含 recall 且给出未完话题时，必须在台词里明确说出该话题。
只输出最终台词，不解释规则。`),
```

这是内嵌在 realize.go 里的第二份提示词，和 system.txt 分工：system.txt 定义人格，这一段定义"表达层的工作边界"。提示词只是软约束，真正兜底的是第 5 段的验证器。

### 3. 任务书注入：锚点与禁语是结构化字段（backend/internal/companion/realize.go:44-57）

```go
		{
			Role: "user",
			Content: fmt.Sprintf(`用户原话：%s
意图：%s
沟通动作：%s
关系阶段：%s
修复类别：%s
可自然引用：%s
表达目标：%s
可靠底稿：%s
必须保留：%s
禁止新增：%s
禁止触碰的话题：%s
最多句数：%d
```

"必须保留"来自 `ContentPolicy.RequiredAnchors`，比如 compactAnchors 产出的"法比安、1-0"；"禁止新增"来自 ForbiddenClaims（new_score/new_player/new_event 等，见 agent.go:2702）。LLM 的输入里，事实是填空题答案，不是作文素材。

### 4. 事实语言兜底闸门（backend/internal/companion/agent.go:1045-1050）

```go
	if intent != IntentPersonalShare && containsMatchFactLanguage(req.Text) && len(requiredAnchors) == 0 {
		allowRealize = false
		if deterministicReason == "policy" {
			deterministicReason = "fact_language_policy"
		}
	}
```

用户话里有"助攻/比分/红牌"这类词、但检索不到任何锚点时，整轮禁用 LLM，直接输出确定性底稿。`allowRealize`/`deterministicReason` 在 agent.go:816-823 初始化，各 intent 分支（claim_policy/fact_policy/snapshot_integrity）提前关闸。这是防幻觉的最后一道结构化防线，不依赖提示词自觉。

### 5. 验证失败，打回底稿（backend/internal/companion/agent.go:2572-2584）

```go
	if err != nil || strings.TrimSpace(realized.Text) == "" {
		if err != nil {
			trace.Error = strings.TrimSpace(err.Error())
		}
		trace.Reason = "realize_fallback_error"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "realizer_error"}})
		return reliable
	}
	allowedSource := strings.Join(compactAnchors(req.Text, reliable, strings.Join(anchors, " ")), " ")
	if err := validateRealizedText(realized.Text, allowedSource, decision); err != nil {
		trace.Reason = "realize_fallback_policy"
		trace.ToolCalls = append(trace.ToolCalls, ToolCall{Name: "response.emit_companion_reply", Args: map[string]string{"mode": "deterministic", "fallback": "policy"}})
		return reliable
```

验证器逐条检查：字数句数、问句数量、锚点是否保留、是否新增比赛事实/新球员/禁话题、越界称呼、脏话分级（agent.go:2684-2729）。任何一条不过，回复静默降级为 reliable 底稿，trace 里留下 `realize_fallback_*` 供事后审计。用户永远收到的是"事实正确但可能平淡"的话，而不是"流畅但编造"的话。

## 评测联动

- 单测：`cd backend && go test ./internal/companion -run 'TestAgentKeepsMatchFactsDeterministicWithRealizerConfigured|TestValidateRealizedTextEnforcesFactLanguageAndRelationshipBoundaries' -v -count=1`。前者配了一个只会胡说"德国已经3比0领先了。"的 fake realizer，断言比分回答不被改写且 reason=`deterministic_fact_policy`；后者是验证器的表驱动用例（编造"萨拉赫梅开二度"必须报错）。
- 黄金用例：`evals/cases/regression/polisher-anchor-mismatch.json`（id `regression.realizer-fact-bypass`）用脚本替身 realizer 返回"莫拉塔助攻"，断言 `mustNotMention: ["莫拉塔"]` 且事实路径绕过润色；`evals/cases/boundary/user-claim-unverified.json` 要求台词含"还没跟上"、禁止附和"对，佩德里"。
- 运行：`npm run eval:offline`（下一讲细讲四档 tier）。
- 失效时看到什么：若删掉锚点校验，单测表里 "new scoring event forbidden" 等子用例变红；若 realizer 输出绕过了闸门，eval 报告出现 `Category:"truth"`、message 形如 `reply must not mention "莫拉塔"` 的失败。

## 动手作业

1. 先跑基线（应全绿）：`cd backend && go test ./internal/companion -run 'TestAgentKeepsMatchFactsDeterministicWithRealizerConfigured|TestValidateRealizedTextEnforcesFactLanguageAndRelationshipBoundaries' -v -count=1`
2. 打开 `backend/internal/companion/agent.go`，注释掉 validateRealizedText 里 2702-2704 行的"禁止新增比赛事实"检查（`forbidsNewMatchClaims(...) && containsMatchFactLanguage(text)` 那个 if 块）。
3. 重跑第 1 步的命令。预期：`TestValidateRealizedTextEnforcesFactLanguageAndRelationshipBoundaries` 中 "new minute and substitution fact forbidden"、"new scoring event forbidden"、"new injury event forbidden" 三个子用例失败——它们编造的新事实不再被拦截。
4. 想清楚再恢复代码：这道检查在提示词之外，为什么必须存在于代码里？
5. （可选）用 `wc -c backend/prompts/v1.0/system.txt` 复验 438 字节；改一个字再跑 `cd backend && go build ./...`，确认提示词是纯文本资产、改它不需要动任何 Go 代码。

## 延伸

- 事实源章节：`docs/球球课程/chapters/27-dialogue-expression.facts.json`（决策/表达分离、长度硬上限、提问克制）、`28-persona-constitution.facts.json`（提示词硬约束、ForbiddenClaims 注入、fact_language_policy 兜底均为 implemented-at 锚点）。
- 上一讲/下一讲：第 3 讲的调度决定了"这一轮说不说"；下一讲看同一份决定的另一半——PresentationPlan 怎么变成 Live2D 动作。
