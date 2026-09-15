---
id: 57-evals-golden-set
title: Evals 评测、黄金集与自动回归（事实源）
source_chapter: docs/球球全套资料/5.AI Coding工程实践/57-Evals评测、黄金集与自动回归.md
status_summary:
  implemented: 9
  partial: 0
  planned: 0
  concept: 4
---

# Evals 评测、黄金集与自动回归（事实源）

评测体系的真实形态是：13 个版本化 JSON 黄金用例、一个 Go 确定性判定器、一个 84 行的 Node 分层运行器和一条 GitHub Actions 门。旧章设想的"模型裁判、四变形、P0/P1 豁免"都没有实现，本篇只讲实现了的部分怎么咬合。

## 系统实际怎么工作

**黄金集。** `evals/cases` 下三套件共 13 个用例：baseline 3 个（进球助攻跟进、球员时间线与闲聊、语音 ASR 意图）、boundary 6 个（ASR 同音、VAR 更正撤销旧事实、非法事件拒绝、未知事实与命令注入、用户主张冲突、用户主张未证实）、regression 4 个（指代吹捧无证据、导播手动主动线、润色锚点不符、润色供应商回退）。schema 强制 `version: "2026-07"`、suite 枚举与 `^(baseline|boundary|regression)\.[a-z0-9-]+$` 的 id 规则（evals/schema/eval-case.schema.json:5-17）。断言长在用例里：`turns[].expect` 的 mustMention/mustNotMention/requiredTools/forbiddenTools 与 `final` 终态（evals/cases/boundary/correction-revokes-old-facts.json:41-55）。

**判定与记分。** Go 侧 `backend/internal/evals` 的 `gradeTextAndTrace` 逐项检查四类断言（runner.go:194-218），另有语音、主动回合、轨迹合同专用判定（runner.go:130-256）。记分卡输出通过率与 factSafetyRate/trajectoryRate/traceCompleteRate 三率（types.go:104-117，summarize 在 runner.go:303-327）；`backend/cmd/evals` 加载用例、按 -suite 过滤、运行并写 JSON 报告，任何失败 exit 1（cmd/evals/main.go:15-38）。离线运行时 LLM 由 scriptedRealizer 脚本替身代替（runner.go:340）。

**四档 tier。** `scripts/evals/run.mjs`（84 行）按 `--tier` 分层：offline = go test 全量 + `go run ./cmd/evals -suite all` + voice-ui 冒烟（run.mjs:20-22）；pr 在此之上加会话隔离 e2e、runtime e2e、eval-audit 轨迹审计、互动审计与 Playwright（run.mjs:24-39）；nightly 加 companion 基准（count=3）与可选 Postgres 集成测试（run.mjs:62-67）；release 要求 MIMO_API_KEY 并跑真机语音冒烟（run.mjs:57-59）。非 release 档把四个外部 API 密钥清空，保证评测离线可跑（run.mjs:10-18）。package.json 暴露 eval:offline/pr/nightly/release 四脚本（package.json:5-10）。

**CI 门。** `.github/workflows/evals.yml` 在 pull_request、push master 与手动触发时跑单一 pr-evals job：go test → flutter analyze/test/build web → Playwright 安装 → `node scripts/evals/run.mjs --tier pr` → 无条件上传 artifacts/evals 产物（evals.yml:23-44）。门禁效果就是退出码：任何一段抛错即红。

**浏览器旅程。** Playwright 配置 testDir 指向 tests/evals、单 worker、失败留截图与 trace、JSON 报告写 artifacts/evals/browser.json（playwright.config.mjs:4-16）；现有 11 个 spec，覆盖投递去重、语音链路、事实生命周期、观察协调、导播鉴权与控制等端到端旅程。

## 与旧设计的差异

| 旧设计主张 | 状态 | 现实 |
| --- | --- | --- |
| 用例最小 Schema：case_id/collection_version/risk_level P0/preconditions/forbidden/judges（旧章:121-148） | concept | 真实 schema 是 version/id/suite/tags/summary/config/events/turns/final，断言在 turns[].expect；无风险分级字段，"blocker" 退化为 tag（correction-revokes-old-facts.json:6） |
| 确定性 + 语义模型判定双轨、低置信人工复核（旧章:53,199-211） | concept | 全部判定确定性（runner.go:194-218）；LLM 用 scriptedRealizer 替身（runner.go:340）。真实对应物：模型真实验证仅 release 档 MiMo 语音冒烟（run.mjs:57-59），不承担裁决 |
| 黄金集六域 mindmap（facts/control/relationship/privacy/media/operations） | concept | 真实划分是三套件 baseline/boundary/regression，域概念以 tags 表达（如 authority/correction/memory/blocker） |
| 每场景状态/时序/媒介/措辞四种变形 + 容差字段（旧章:151-166） | concept | schema 无变形字段；对抗变形以独立 boundary 用例近似：asr-homophone-assist（措辞/同音）、unknown-fact-and-command-injection（输入变形） |
| P0/P1 硬门、回归门、覆盖门、人工门五层门禁与豁免制度（旧章:264-313） | concept | 只有一条门：用例失败 → exit 1 → CI 红（cmd/evals/main.go:35-37，evals.yml:38）；无豁免登记、无基线管理、无退役流程 |
| 离线/集成/端到端三层运行（旧章:34-51） | implemented-at（命名不同） | 真实分层是四档 tier：offline⊂pr⊂nightly/release（run.mjs:6,24,57,62），映射关系为离线判定/集成 e2e/浏览器旅程 |
| 定时全量流水线 | partial | nightly 逻辑存在（run.mjs:62-67），但无 GitHub schedule 触发器，需自建定时 |
| "13 个 Playwright 旅程"类计数 | 漂移 | 13 是黄金用例数；Playwright spec 是 11 个（tests/evals） |

## 主张-锚点表

| # | 主张 | status | 锚点 |
| --- | --- | --- | --- |
| 1 | 13 个黄金用例，3/6/4 三套件 | implemented-at | evals/cases/baseline; evals/cases/boundary; evals/cases/regression; correction-revokes-old-facts.json:1-7 |
| 2 | schema 强制 2026-07 版本与 id 规则 | implemented-at | evals/schema/eval-case.schema.json:5-17 |
| 3 | 旧 §3.2 用例字段与真实 schema 不同 | concept | 旧章:121-148; eval-case.schema.json:5-17; correction-revokes-old-facts.json:41-55 |
| 4 | 四类确定性断言判定器 | implemented-at | backend/internal/evals/runner.go:194-218; backend/internal/evals/journey_runner.go:10 |
| 5 | 模型裁判/语义判定/人工复核不存在 | concept | runner.go:340; 旧章:53,199-211 |
| 6 | 记分卡三率 | implemented-at | backend/internal/evals/types.go:104-117; runner.go:303-327 |
| 7 | 失败 exit 1 + gitRevision 报告 | implemented-at | backend/cmd/evals/main.go:15-38; types.go:119-127 |
| 8 | 四档 tier + 四个 npm 脚本 | implemented-at | scripts/evals/run.mjs:6,20-22,24-39,57-67; package.json:5-10 |
| 9 | 非 release 档密钥清零 | implemented-at | scripts/evals/run.mjs:10-18 |
| 10 | nightly 基准与 Postgres 集成测试 | implemented-at | scripts/evals/run.mjs:62-67 |
| 11 | CI pr 门 + 产物上传 | implemented-at | .github/workflows/evals.yml:3-12,23-44 |
| 12 | 11 个 Playwright 旅程 spec | implemented-at | playwright.config.mjs:4-16; tests/evals; tests/evals/fact-lifecycle.spec.mjs |
| 13 | 变形制度/豁免制度/退役流程无载体 | concept | 旧章:151-195,288-313; evals/cases/boundary/asr-homophone-assist.json; evals/cases/boundary/unknown-fact-and-command-injection.json |
