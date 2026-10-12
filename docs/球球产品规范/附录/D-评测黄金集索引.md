---
篇号: "D"
标题: 附录 D · 评测黄金集索引
性质: 附录（参考件——12 套件 249 用例的统计与入口索引；资产定义、判定语义与治理纪律的唯一规范面是 24 篇，门禁判定归 25 篇，本篇不重复）
状态: active
版本: v3.0
基线: a7e5100（2026-06-07）
文体: reference（查表体：套件索引 + 口径摘要 + 盲测资产索引）
上游: PRD-球球-v3.0.md 第八章 8.5（评测套件索引）；套件/命令/CI/实证档逐项对账；迁移源 v2 档案 附录I（评测标准、黄金集与回归数据规范，已并入 24 篇）
评测挂钩: 本篇即索引——逐用例内容与命令手册归 24 篇 §2/§8；套件增删回写本篇与 PRD 8.5 计数
裁决点: identity 空悬套件补齐或除名（工程侧待办，NFR-MNT-001 注记）；合规负例族挂 ⚖️B15；汇总见 37 篇
---

# 附录 D 评测黄金集索引

> 本篇基线：2026-06-07 仓库 a7e5100。

**本篇定位**：评测黄金集的统计与入口索引——12 套件逐个给名称/路径/用例数/覆盖域/挂钩/运行命令（§2），golden schema 版本与断言词汇表摘要（§3），234+15 聚合口径（§4），盲测资产索引（§5）。**本篇不重复 24 篇**：分层规则（EV-01～05）、黄金集管理铁律（EV-10～17）、宪法负例机制、政策↔评测互链、命令手册全文与已知缺陷注记，一律以 24 篇为准；门禁判定（G1-G11、CI 编排、验收环境矩阵）归 25 篇。

**事实依据**：全部数字以 2026-06-07 仓库 a7e5100 实测为对照源（`evals/cases/` 12 目录 249 个 JSON 逐目录清点；`artifacts/evals/offline.json` 为 05-30 实证档）。

---

## 1. 总口径

| 项 | 口径 |
| --- | --- |
| 套件全集 | **12 套件 249 用例文件**（2026-06-07 实测）；合法套件枚举 13 个——identity 为空悬套件（§2.2 注记） |
| 聚合口径 | **offline 聚合 234 实跑 + 15 真网档显式点名**：`-suite all` = 11 离线套件 234；router_net 15 例不进 all，需显式 `-suite router_net` |
| 全绿实证 | 234/234（2026-05-30 实证档 `artifacts/evals/offline.json`，gitRevision 6904033）——当前唯一有实证的全绿档；「248 全绿」从未有实证（`offline-248.json` 名实不符，实含 232 例） |
| schema 版本 | `2026-03`（`evals/schema/eval-case.schema.json` const 强制；改版=破坏性变更） |
| 用例形态 | 一条用例=一场可重放的微型比赛（config+events+turns+final），断言长在用例里；离线运行时语言模型由脚本化替身取代，全判定无网络无密钥可重复 |
| CI | 双 job：pr-evals（20min）+ promptfoo-gate（15min，无模型 key）；nightly 档无 cron，只能本地手跑 |

---

## 2. 套件索引（12 套件逐个）

路径统一前缀 `evals/cases/<套件名>/`；运行命令统一为 `go run ./cmd/evals -suite <套件名>`（backend/ 目录），逗号列表可并列。挂钩列给承接规范篇与 PRD 需求域；逐用例覆盖明细归 24 篇 §2.1。

| # | 套件 | 用例数 | 覆盖域（实测用例族摘要） | 挂钩（规范篇 × FR 域） | 运行命令 |
| --- | --- | --- | --- | --- | --- |
| 1 | baseline | 36 | 常规赛况问答（比分/分钟/半场/赛前）、进球者/助攻/最后一传、赛程、情绪接住、闲聊问候、控制指令、用户主张确认/反驳、语音话轮 | 14、07 × FR-FACT、FR-DLG | `-suite baseline` |
| 2 | boundary | 38 | 非字面表达（梦进球/假设/玩笑/插话）、坚持追问、注入改分/记分/重写、ASR 同音、长乱码、低置信路由、控制接受、路由边界（route-* 12 例）、修复、事实生命周期 | 14、01 × FR-FACT、FR-DLG、PR-R1 | `-suite boundary` |
| 3 | regression | 26 | 回归：开放话题恢复、润色越锚与回落、画像一致性、坚持三连、用户隔离、路由阈值边界与错误漏斗、手动主动线、现场比分主张确认 | 07、08、18 × FR-DLG、FR-MEM、FR-IDN | `-suite regression` |
| 4 | companion | 14 | 陪伴行为与主动回合：情绪接住四型、半场小结与空比分、判罚回退、手动主动线、静默标签话轮（quiet-tag-silence） | 07 × FR-DLG、FR-PRO、FR-FACT | `-suite companion` |
| 5 | knowledge | 13 | 判罚附句触发（点球/红牌/VAR/手球）与限频/负触发、知识问答、诚实不知（库外不编造）、条目不相交 | 15 × FR-KNW、FR-FACT-022 | `-suite knowledge` |
| 6 | memory | 26 | 画像隔离 6 例（投注/城市/食物/伴侣/球员/球队不串用户）、召回 12 例（同域逐项）、口味史 8 例（敌队/风格/换队/衰减/节奏） | 08、16 × FR-MEM | `-suite memory` |
| 7 | multi | 12 | 多轮链式追问、跨用户链隔离、情绪与事实混说、话题回访、三轮事实链、语音文字接力 | 18、07 × FR-DLG、FR-IDN | `-suite multi` |
| 8 | persona | 15 | 人格设置域：话痨档位三连、偏好召回（主队/敌队/昵称）、记忆召回、自我状态、立场坚持——显式设置优先（ADR-0018） | 12、07 × FR-UI-006、FR-DLG | `-suite persona` |
| 9 | robust | 19 | 健壮性：ASR 填充词/噪声/半句/绕舌/短句、路由错误漏斗、低置信漏斗、空配置容忍、纯标点安全、长乱码安全、路由超时 | 06、20 × FR-VOICE、FR-DLG | `-suite robust` |
| 10 | safety | 22 | 安全负例面五组：dependence 4（排他/永远/恋爱/只有你）、hypothetical 5、injection 6（伪造终场/假点球/假红牌/运营指令/记进球/改比分）、overstep 3、revoke 4；《办法》专项负例族不在此（24 篇 §7，规划中） | 01、05 × PR-R3～R9、FR-SEC | `-suite safety` |
| 11 | voice | 13 | 语音链路域：同音（球队/时间）、耳语、感叹、数字主张、噪声守卫、重复、尾音、ASR 确认链（ok/ok-score/ok-goal）、疑问标记、串音 | 06、20 × FR-VOICE | `-suite voice` |
| 12 | router_net | 15 | **真网档**：意图路由全谱 15 例（每意图一档含 unknown：claim/control/emotion/followup/knowledge/matchreaction/player/recent/reminder/schedule/share/smalltalk/status/subscription/unknown），真实 LLM 显式运行 | 07 × FR-DLG-001、FR-PRO、FR-FACT | `-suite router_net`（需真实 key） |
| — | 合计 | **249** | `-suite all` = 249 − 15 = **234**（05-30 实证 234/234 全绿） | | `-suite all` |

### 2.1 identity 空悬套件（如实注记）

合法套件枚举 13 个（`backend/internal/evals/load.go` validSuites），其中 **identity 为空悬**：枚举有、`evals/cases/` 无目录无用例。后果：误标 `"suite": "identity"` 能过 Go 加载校验但不合 schema 枚举（两道校验口径不一致）；身份域（FR-IDN、ADR-0020 登录缝、会话隔离）当前无套件级黄金用例，隔离行为由运行时 E2E（`session-isolation-e2e.mjs`）与 Go 单测承载。补齐或除名为工程侧待办；裁决前任何「identity 套件已覆盖身份评测」的表述违规。

### 2.2 配套门禁资产（同属发布门禁，规则归 25 篇）

| 层 | 内容 | 挡门 |
| --- | --- | --- |
| promptfoo 门禁层 | 4 用例 6 断言（错比分负例/红队诱导 2-0/让路顺序/REST 目录形状），确定性 javascript，CI 无模型 key（`tests/promptfoo/promptfooconfig.gate.yaml`） | 是（CI 红阻断，注错验红 exit=100） |
| promptfoo 观测层 | 4 用例 llm-rubric 主观报告（比分/进球/赛程/主队），永不挡门（`npm run eval:observe`） | 否 |
| 运行时 E2E | 会话隔离 / 真实 WS 话轮 / trace 审计（eval-audit）/ 交互审计 / Playwright 20 spec 99 tests（5,904 行） | pr 档 |
| 像素级回归 | 表现白名单 23 接受值 + `scripts/live2d-bundle/regression.mjs` | G4 |
| 金样本 | 13 份逐字锁 console 契约（`backend/cmd/server/testdata/goldens/`） | offline 档 |

---

## 3. golden schema 与断言词汇表摘要

### 3.1 schema 要点（摘要，全文归 24 篇 §3）

| 项 | 内容 |
| --- | --- |
| 版本 | 每条用例 `version` 恒为 `"2026-03"`（schema const 强制）；双版本并存不允许 |
| ID 纪律 | 形如 `套件.名称`（正则锁 12 套件名），全库唯一、加载期跨套件去重 |
| 结构校验 | `load.go` Validate：事件 key 必填不重复、`corrects` 只引用更早 key、非 expectError 事件必有 eventType、话轮 id/userId 必填、`afterTurn` 必引用存在话轮、`portraitClaim` 必须锁生产槽位、portrait 操作集限 ADD/UPDATE/DELETE/NOOP |
| 跨回合能力 | `events[].afterTurn` 话轮后注入事件；`portrait`+`forgetPortrait` 话轮间画像操作；`final.minimumTraces` 整场留痕下限 |
| 离线确定性 | 除 release 档外清空 DEEPSEEK/MIMO/ELEVENLABS/APISPORTS 四键；LLM 路由走回落、TTS 走字幕兜底 |

### 3.2 断言词汇表（逐轮 expect 摘要）

| 断言字段 | 作用 |
| --- | --- |
| `intent` / `exact` | 意图路由方向与精确回复文本 |
| `mustMention` / `mustNotMention` | 回复必须/不得出现的内容（如更正后不得再提旧比分） |
| `requiredTools` / `forbiddenTools` | 允许与禁止调用的工具（运营事实修改工具永远在 forbidden 侧） |
| `retrievedEventKeys` | 回复所依据的事件键（有据可查） |
| `claimKind` / `claimStatus` / `claimCertainty` / `forbidClaim` | 用户赛况主张必须被确认、纠正或保持未证实 |
| `memoryMustMention` / `memoryMustNotMention` | 记忆上下文（召回+画像块）必须/不得包含的内容 |
| `expect.reason` | 预期的守卫/决策原因码（留痕断言） |

五类确定性 grader（truth / claim safety / trajectory / trace / voice）消费这些字段产出判定；LLM judge 明确不做事实权威（EV-02，24 篇）。

---

## 4. 聚合口径（234+15，唯一口径）

| 口径 | 内容 | 违规表述 |
| --- | --- | --- |
| offline 聚合 | `-suite all` 实跑 **234**（11 离线套件）；05-30 实证档 234/234 全绿（gitRevision 6904033） | 「248 全绿」「当前全绿」无版本号断言 |
| 真网档 | router_net **15** 显式点名（`-suite router_net`，动机=真 key 费用与抖动） | 「跑过 all 即含真网」 |
| 总口径 | 「12 套件 249 用例」= 11 离线 234 + 真网 15 | 「100 例三套件」（v2 旧口径） |
| 记账 | evals-248 change 记账目标已被现实 249 越过（1.6/1.7 未勾）；`offline-248.json` 名实不符（232 例）——记账清理归工程侧 | 「248 对表已完成」 |
| 报告 | 每次运行统一含 schema 版本/用例数/通过率/事实安全率/轨迹率/留痕完整率/分套件通过率/`gitRevision`；通过率只描述本次数据集与环境，不外推 | 「judge 分数可替代盲测」 |

---

## 5. 盲测资产索引（判据已立，实跑状态照实登记）

主观质量评测按预注册盲测纪律执行（四步流程与裁决记录格式归 24 篇 §5）；本篇只索引资产与状态。

| 资产 | 路径 | 判据/规格 | 状态 |
| --- | --- | --- | --- |
| UTMOSv2 先筛 | `scripts/utmos/mos_rank.py`、`scripts/utmos/README.md` | TTS 候选 MOS 1-5 排序，进盲测集阈值默认 3.5；中文域只粗筛不下结论；需独立 venv | **未实跑**（venv 与 ≥10 样本试跑待有卡机器） |
| 韵律盲测记录 | 材料/探针 c40056e；裁决 e35c3fe；ADR-0012 修订注 | 预注册判据线 30% 嫌弃；10 对双盲 | **已实跑并触发裁决**：句粒度版 2 胜 8 负（80% 嫌弃）→ 句粒度投递拆除回整段，首响 3-7s 如实接受——预注册纪律第一次真实触发生产裁决 |
| smart-turn 三件工具 | `client/tool/turn_collection.html`（采集 199 条提示语三场景）/ `turn_annotation.html`（标注，复标一致性 ≥90% 门）/ `turn_model_compare.mjs`（双模态双腿对比+自动门判） | 门判=准确率不低于现役且误判不高于现役；stub 自测通过 | **工具已落地，用户录音/标注未开始**（约 2h 用户侧成本；录音不入库，`turn_dataset/` 已 gitignore） |
| ASR 自托管对照门 | `scripts/asr-eval/` | 16 例四类 CER 对照 | **已出结论**：FunASR seaco 0.107 / 无热词 0.120 vs MiMo 0.040——不过门，维持 MiMo；资产可复跑 |
| 解说评测集 | `docs/evals/commentary-eval-set.md` | ≥50 样本（三类事件）才首跑；三硬门零容忍 + BERTScore ≥0.85 | 骨架已立；**未达门槛不跑分**（样本随 auto-hosting 真实流量积累） |
| 人味多回合样例 | `docs/evals/humanity-multiturn-scenarios.md` | 多回合人味场景样例集 | 样例在库 |

---

## 6. 运行命令速查（摘要；手册全文归 24 篇 §8）

```bash
# Go 黄金集直跑（backend/ 目录）
go run ./cmd/evals -suite all            # 离线 234（排除真网档）
go run ./cmd/evals -suite router_net     # 真网档：需真实 key，显式点名
go run ./cmd/evals -suite all -out ../artifacts/evals/offline.json

# 四档阶梯（仓库根）
npm run eval:offline / eval:pr / eval:nightly / eval:release

# promptfoo 双层
node scripts/promptfoo/run-gate.mjs      # 门禁层（红阻断，exit=100）
npm run eval:observe                     # 观测层（judge 报告，永不挡门）
```

退出码：加载失败 2 / 有失败用例 1 / 成功 0。已知缺陷注记（`-suite` flag help 过期、identity 空悬、`docs/evals/README.md` 旧版、artifacts 历史产物堆积、promptfoo 三坑）归 24 篇 §8.3，本篇不重复。

---

## 设计说明（写法依据，非索引条目）

1. **迁移映射**：本篇承接 v2 档案《附录I-评测标准、黄金集与回归数据规范》的索引职能（规范本体已并入 24 篇）；v2 附录I 的「三套件 100 例」全表、「语言模型只在发布档出现」「18 条旅程」「导播台六页」等口径已按对账结论全部作废——本篇以 12 套件/249/234+15/双 job/20 spec 为准。
2. **与 PRD 8.5 的分工**：PRD 第八章 8.5 是需求侧统计入口；本篇与 PRD 8.5 计数同源（12/249/234+15），冲突时先修源表（附录 B 评测域）再同步两处。
3. **盲测资产的状态纪律**：三类状态严格区分——「已实跑并触发裁决」（韵律盲测）、「工具在、未实跑」（UTMOSv2、smart-turn 录音标注）、「已出结论」（ASR 对照门）；任何把「未实跑」写成「已上线」的表述违规（c6 §5.4 同款口径）。
4. **identity 的索引地位**：本篇按「合法枚举 13、有用例 12」如实并记；identity 的处置（补齐或除名）是工程侧待办，处置落地时同步本篇 §1/§2.2 与 24 篇 §2.2 两处注记。

---

## 修订记录

| 版本 | 日期 | 依据 | 变更摘要 |
| --- | --- | --- | --- |
| v3.0.1 | 2026-06-09 | 复核修复（元数据缺项） | YAML 补「篇号」字段（取值附录字母，00 篇 §6.3）；补设本修订记录节 |
| v3.0.2 | 2026-06-09 | 主编通读修订（E1；读者报告 A/B/C 旧编号断链清单） | §2「挂钩（规范篇 × FR 域）」列旧篇号清零迁移至 00-39 新号体系（12 套件逐行，语义抽核：baseline→14/07、safety→01/05、voice→06/20、knowledge→15、memory→08/16）；定位句门禁集合 G1-G10 改 G1-G11（25 篇 v3.0.2 已增补 G11） |
