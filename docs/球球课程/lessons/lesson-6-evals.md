---
lesson: 6
title: 质量层：怎么证明这套东西没坏
trace_position: trace 第 7 站。上游：前六站的每一步输出（台词、表演、轨迹）；下游：exit 1 门禁决定这些输出敢不敢进 master
depends_on: [57-evals-golden-set]
---
# 第 6 讲 · 质量层：怎么证明这套东西没坏

## 这一站在 trace 上

前六站每一步都可能是幻觉入口：LLM 润色、事件映射、白名单消费。本站不产出一行业务逻辑，只回答一个问题：你怎么知道"刚才是谁助攻的？"永远得到"法比安"，而不是莫拉塔、不是模型即兴发挥。答案是 13 个版本化 JSON 黄金用例、纯确定性判定器、四档运行 tier 和 exit 1 门禁。没有模型裁判，没有语义评分——判定器只做字符串和轨迹比对。

```
evals/cases/*.json ─> LoadCases+Validate ─> Run（内存 Store + 脚本替身 realizer）
      ▲                            │ gradeTextAndTrace 四类断言 → Scorecard 三率
npm run eval:offline/pr/nightly/release ◄─ FailedCases>0 → exit 1 → CI 红
```

## 证据锚点

- `evals/cases/baseline/`（3 个）、`boundary/`（6 个）、`regression/`（4 个）— 13 个黄金用例；`evals/schema/eval-case.schema.json:5-17` version 固定 "2026-07"、id 模式 `^(baseline|boundary|regression)\.[a-z0-9-]+$`、additionalProperties: false；Go 侧双保险 `backend/internal/evals/load.go:48-88`
- `backend/internal/evals/runner.go:194-218` — gradeTextAndTrace：mustMention/mustNotMention/reason/requiredTools/forbiddenTools；scriptedRealizer 在 runner.go:338-345，LLM 在评测里是脚本替身不是裁判
- `backend/internal/evals/types.go:104-117` — Scorecard：passRate + factSafetyRate/trajectoryRate/traceCompleteRate 三率；summarize 在 runner.go:303-336 按失败 Category 分别结算
- `backend/cmd/evals/main.go:35-37` — FailedCases>0 → os.Exit(1)，门禁是退出码
- `scripts/evals/run.mjs:6-22` — tier 参数；非 release 档清空 DEEPSEEK/MIMO/ELEVENLABS/APISPORTS 密钥；24-55 pr 档、57-60 release 档、62-67 nightly 档
- `package.json:5-10` — eval:offline / eval:pr / eval:nightly / eval:release
- `.github/workflows/evals.yml:3-7,38-44` — pull_request / push master / workflow_dispatch 触发；`--tier pr` + `if: always()` 上传 artifacts/evals
- `tests/evals/`（11 个 spec）+ `playwright.config.mjs:7-15` — 单 worker、失败截图/trace、JSON 报告到 artifacts/evals/browser.json

## 代码走读

### 1. 用例即数据：期望写在用例里（evals/cases/baseline/goal-assist-follow-up.json:52-58）

```json
      "expect": {
        "intent": "recent_event_question",
        "mustMention": ["法比安", "亚马尔"],
        "requiredTools": ["match.search_events", "conversation.append_turn", "trace.write_decision", "response.emit_companion_reply"],
        "forbiddenTools": ["operator.create_event", "operator.correct_event"],
        "retrievedEventKeys": ["pedri-goal"]
      }
```

同一份文件还注入佩德里进球事件（participants 带 scorer/assist/pre_assist）和主动线 expectation。答案是期望，不是输出——运行时把整条轨迹（调了哪些工具、检索了哪个事件）一起对照；禁止 eval 调 operator 写接口，权限边界也被用例锁住。

### 2. 判定器：纯文本/轨迹断言（backend/internal/evals/runner.go:194-218）

```go
func gradeTextAndTrace(result *StepResult, reply string, trace companion.Trace, mustMention, mustNotMention []string, reason string, requiredTools, forbiddenTools, eventKeys []string, eventIDs map[string]string) {
	for _, value := range mustMention {
		if !strings.Contains(reply, value) {
			result.Failures = append(result.Failures, Failure{Category: "truth", Message: fmt.Sprintf("reply must mention %q", value)})
		}
	}
	for _, value := range mustNotMention {
		if strings.Contains(reply, value) {
			result.Failures = append(result.Failures, Failure{Category: "truth", Message: fmt.Sprintf("reply must not mention %q", value)})
		}
	}
	if reason != "" && trace.Reason != reason {
		result.Failures = append(result.Failures, Failure{Category: "trace", Message: fmt.Sprintf("reason got %q, want %q", trace.Reason, reason)})
	}
	toolSet := toolsIn(trace)
	for _, name := range requiredTools {
		if !toolSet[name] {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("missing required tool %q", name)})
		}
	}
	for _, name := range forbiddenTools {
		if toolSet[name] {
			result.Failures = append(result.Failures, Failure{Category: "trajectory", Message: fmt.Sprintf("called forbidden tool %q", name)})
		}
	}
```

没有 embedding，没有 LLM judge。每条失败带 Category（truth/trajectory/trace/claim_safety/voice）。确定性判定的代价是表达力：锁得住"必须提法比安、不许提莫拉塔"，锁不住"说得自不自然"——这个取舍是整套评测设计的核心。

### 3. 记分卡与门禁（backend/internal/evals/types.go:104-113 + backend/cmd/evals/main.go:35-37）

Scorecard 的三个率各答一问：factSafetyRate 答"有没有胡说"，trajectoryRate 答"路径对不对"，traceCompleteRate 答"能否事后审计"。summarize（runner.go:303-336）按失败 Category 分别结算，文本全对但少调工具的用例 factSafety 是 1、trajectory 不是——失败不互相稀释。门禁不在记分卡里，在入口程序的退出码里：

```go
	if report.Scorecard.FailedCases > 0 {
		os.Exit(1)
	}
```

报告带 gitRevision 写入 JSON 工件（main.go:31-32）之后才看失败数：任何一条失败，进程 exit 1，CI 的 run 步骤直接红。评测从"参考"变"门禁"靠的就是这一行，不是文档约定。

### 4. 四档 tier：同一入口，不同深度（scripts/evals/run.mjs:10-22）

```javascript
const evalEnvironment = tier === 'release'
  ? { ...process.env }
  : {
      ...process.env,
      DEEPSEEK_API_KEY: '',
      MIMO_API_KEY: '',
      ELEVENLABS_API_KEY: '',
      APISPORTS_API_KEY: '',
    };

await runCommand('go', ['test', './...'], { cwd: backendDir, env: evalEnvironment });
await runCommand('go', ['run', './cmd/evals', '-suite', 'all', '-out', '../artifacts/evals/offline.json'], { cwd: backendDir, env: evalEnvironment });
await runCommand('node', [join('scripts', 'voice-ui-smoke.mjs')], { cwd: repoRoot, env: evalEnvironment });
```

offline 档清空四个外部密钥：不花钱、不出网就能全量跑黄金集。pr 档加会话隔离/runtime e2e/轨迹审计/Playwright（24-55 行），nightly 加基准与 Postgres 集成测试（62-67 行），release 反向要求 MIMO_API_KEY 跑真机语音冒烟（57-60 行）。原则：越贵的验证越靠后，黄金集永远先跑。

## 评测联动

- 谁评测评测系统：`backend/internal/evals/runner_test.go:23-25` 锁确定性判定三率必须为 1，`TestLoadCasesRejectsUnknownSuite` 锁 suite 枚举。CI 侧 evals.yml 在 go test + flutter analyze/test/build web 之后跑 `node scripts/evals/run.mjs --tier pr`。
- 与第 4 讲联动：`evals/cases/regression/polisher-anchor-mismatch.json`（id `regression.realizer-fact-bypass`）用脚本替身 realizer 喂"莫拉塔助攻"，断言 `mustNotMention: ["莫拉塔"]` + `reason: deterministic_fact_policy`——润色层任何一次自由发挥都会变成 exit 1。
- 浏览器旅程：`npx playwright install chromium` 后 `node node_modules/@playwright/test/cli.js test --config=playwright.config.mjs`（run.mjs:50 的调用形式），11 个 spec 覆盖进球跟进、投递去重、观察协调、导播控制。
- 失效时看到什么：把任一用例 mustMention 改成错误球员，`npm run eval:offline` 以 exit 1 结束，`artifacts/evals/offline.json` 该 case 的 steps 出现 `{"category":"truth","message":"reply must mention …"}`；JSON 写坏 schema（version 不是 "2026-07"）则在跑任何用例前 exit 2。

## 动手作业

1. 基线（需 Go 工具链；offline 档已自动清空外部密钥）：`npm run eval:offline`，预期 scorecard `passRate: 1`、13 用例全过。
2. 新建 `evals/cases/regression/score-question-after-goal.json`（id 必须匹配 `^regression\.[a-z0-9-]+$`，version 固定 "2026-07"，照抄 schema 硬约束）：

```json
{
  "version": "2026-07",
  "id": "regression.score-question-after-goal",
  "suite": "regression",
  "summary": "佩德里进球后问比分，回答必须引用快照 1-0 与事件时钟。",
  "config": {"homeTeam": "西班牙", "awayTeam": "德国"},
  "events": [{"key": "goal", "event": {"eventType": "goal", "period": "first_half", "clock": "24:10", "teamId": "home", "teamName": "西班牙", "playerName": "佩德里", "score": {"home": 1, "away": 0}, "intensity": 5, "description": "佩德里破门。", "visibility": "public"}}],
  "turns": [{"id": "ask-score", "userId": "fan-1", "text": "现在几比几？", "expect": {"intent": "match_status_question", "mustMention": ["西班牙", "1-0", "德国", "24:10"], "requiredTools": ["match.read_snapshot"]}}],
  "final": {"score": {"home": 1, "away": 0}, "minimumTraces": 1}
}
```

3. 单跑回归套件验证新用例：`cd backend && go run ./cmd/evals -suite regression -out ../artifacts/evals/regression.json`（`-suite`/`-out` 参数见 main.go:16-18），然后查 `artifacts/evals/regression.json` 里 `regression.score-question-after-goal` 的 `"passed": true`。expected 的三类断言对应真实行为：比分回答由 agent.go:909 的快照模板产出，必含主客队、`1-0` 和事件时钟 `24:10`。
4. 故意写坏一次：把 version 改成 `"2026-08"` 重跑第 3 步。预期 stderr 报 `evals: validate ../evals/cases/regression/score-question-after-goal.json: version must be "2026-07"` 且 exit 2——LoadCases 在跑任何用例之前就拒绝整个目录。改回后重跑确认恢复。
5. 全量确认新用例入集：`npm run eval:offline`，预期 `totalCases: 14, passRate: 1`。

## 延伸

- 事实源章节：`docs/球球课程/chapters/57-evals-golden-set.facts.json`——13 用例分布、schema 约束、四类判定器、四档 tier、exit 1 门禁、11 个 Playwright spec 全带锚点；"语义判定器/人工复核/风险分级"被如实标为 concept，别在旧文档里找它们。
- 回看第 4/5 讲的失效模式：能被字符串断言锁住的（不许提莫拉塔、必须执行 cheer）都进了黄金集；锁不住的（语气自然度）是这套系统的已知边界。叙事呼应：`docs/直播课-15张架构图逐字稿-非技术版.md` 图 11"Release Evals……门禁失败以后进入收缩/回滚"。
