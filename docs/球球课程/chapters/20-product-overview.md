---
id: 20-product-overview
title: 产品设计总览（事实源）
source_chapter: docs/球球全套资料/4.产品设计/20-产品设计总览.md
status_summary:
  implemented: 9
  partial: 2
  planned: 0
  concept: 3
---

# 产品设计总览（事实源）

球球是一个 AI 足球陪看数字人：Live2D 形象、实时语音、围绕真实比赛事件回应。本篇不再复述旧总纲的设想，而是回答一个问题：当旧总纲写下"任务、控制、事实、关系"四组承诺时，今天的仓库里到底是什么在兑现它们。一切以 `CONTEXT.md` 术语表与真实代码为准。

## 系统实际怎么工作

**一个单体，五条内部链。** 生产入口是 `backend/cmd/server` 单体（backend/Dockerfile:6），在一个 HTTP mux 上同时挂用户会话（`/api/sessions/`）、隐私权利（`/api/me/`）、比赛目录与事实（`/api/matches/`）、企业导播台页面（`/operator.html`）和 WebSocket（`/ws/match/`）（backend/cmd/server/main.go:386-425）。`backend/cmd` 下另有四个工具入口：evals（离线评测）、eval-audit（轨迹审计）、latency-test、verify-datasource。部署是 docker-compose 三容器：backend、`pgvector/pgvector:pg16`、redis（docker-compose.yml:1-49）。

**领域分包。** 后端逻辑按 21 个 internal 包组织：matchstate（事实账本）、companion/conversation（决策与调度）、relationship（关系阶段）、observation（直播观察协调）、privacy、operatorwrite/directordraft（导播）、asr/llm/tts/pipeline（语音链）、interaction、evals 等（backend/internal，backend/cmd/server/main.go:22-42 的 import 即真实依赖清单）。

**产品语言。** 术语以根目录 CONTEXT.md 为唯一词典：Digital Ballmate（CONTEXT.md:7-9）、Relationship Stage 四阶段（CONTEXT.md:17-19，枚举实现在 backend/internal/relationship/types.go:100-106）、Match Fact / Fact Claim（CONTEXT.md:85-92，事实账本在 backend/internal/matchstate/fact_ledger.go，公共读走账本重放并可开关回退，README.md:136-139）、Watch Turn / Delivery Outcome / Interaction Ledger（CONTEXT.md:93-103）。

**客户端。** Flutter 端 lib 下分 screens/services/widgets/theme 四层（client/lib），设置页提供话痨程度（安静/刚好/热闹三档，client/lib/screens/settings_screen.dart:96-100）、连续对话、字幕、声音开关与支持球队（settings_screen.dart:111-160）。语音链路 VAD→ASR→事件融合→LLM→TTS→Live2D（README.md:106-115），MiMo 统一供对话与语音（docker-compose.yml:18-21）。

**图与文档的事实源。** 架构图是 `architecture/archify/*.json` 15 张图源，showcase 生成页不入库（architecture/README.md:7-16）；架构图集 README 逐图给出对应代码锚（如 agent-core.json 对应 backend/internal/companion/agent.go）。

## 与旧设计的差异

| 旧设计主张 | 状态 | 现实 |
| --- | --- | --- |
| 同卷资料描述的 `cmd/scenario`、`cmd/gateway`、`cmd/realtime`、`cmd/facts` 微服务拆分 | concept | 从未存在。真实形态是 cmd/server 单体 + 4 个 CLI 工具（backend/cmd）；ADR-0004 明确以单体为准（docs/adr/0004-content-constitution-evidence-anchors.md:31-34）。`backend/main.go:20-28` 是仅含 /health 与 /ws 的残留入口 |
| 「四维统一控制模型」：陪看密度/信息保护/声音方式/动态强度四个独立维度 | partial | 客户端只落地了其中两个方向：话痨程度 3 档与声音/字幕开关（settings_screen.dart:93-160）；「信息保护」无独立设置项，防剧透由服务端观察协调承担（PENDING_OBSERVATION_COORDINATION，docker-compose.yml:26）；「动态强度」无设置项 |
| talkativeness 0-10 十档（README:117-127） | partial | 客户端实现是 quiet/normal/active 三档（settings_screen.dart:96-100，preferences_service.dart:30），backend 代码不读取该字段，档位表是文档残留 |
| PR-01..PR-10 验收矩阵编号 | concept | 仓库无此编号体系；同类风险改由可执行断言承载：evals/cases/boundary 下 6 个对抗用例覆盖事实更正、剧透、命令注入、未证实主张 |
| 能力成熟度 L1 拉取/L2 许可/L3 受限主动 | concept | 无分级资格链；主动发言是 conversation 调度的一种行为，由 evals/cases/regression/manual-proactive-line.json 回归保护 |
| 商业探索层、适龄/未成年人流程 | concept | 仓库无任何商业与适龄实现，旧文自身也标注"待验证" |

保留的真实领域语言：六承诺、T1-T4 任务、非目标清单在精神上被后续章节（23/24/27/29/31 等事实源章节）逐条兑现，但旧总纲作为"总纲"不直接对应任何单一模块，本篇因此围绕真实系统形态重写。

## 主张-锚点表

| # | 主张 | status | 锚点 |
| --- | --- | --- | --- |
| 1 | 定位 Digital Ballmate，非主播/助手/恋人 | implemented-at | CONTEXT.md:7-9; PRODUCT.md:17-19; README.md:1-3 |
| 2 | Go 单体 + Flutter 客户端 + 三容器部署 | implemented-at | backend/cmd/server/main.go:386-425; backend/Dockerfile:6,21; docker-compose.yml:1-49; client/lib |
| 3 | cmd/scenario 等微服务从未存在 | concept | backend/cmd; docs/adr/0004:31-34 |
| 4 | internal 下 21 个领域包 | implemented-at | backend/internal; backend/cmd/server/main.go:22-42 |
| 5 | VAD→ASR→LLM→TTS→Live2D 语音链 | implemented-at | README.md:106-115; backend/internal/asr; backend/internal/tts; client/lib/services/vad_service.dart; client/lib/services/presentation_state.dart |
| 6 | 关系阶段四态枚举 | implemented-at | CONTEXT.md:17-19; backend/internal/relationship/types.go:100-106 |
| 7 | Match Fact 账本与公共读开关 | implemented-at | CONTEXT.md:85-92; backend/internal/matchstate/fact_ledger.go; backend/migrations/022_fact_ledger_recorded_sequence.sql:1-10; README.md:136-139 |
| 8 | 四维控制模型仅部分落地 | partial | client/lib/screens/settings_screen.dart:93-160,96-100 |
| 9 | 话痨 0-10 档为文档漂移 | partial | README.md:117-127; settings_screen.dart:96-100; client/lib/services/preferences_service.dart:9,30; client/lib/screens/match_screen.dart:568,895 |
| 10 | PR-01..PR-10 编号矩阵无对应物 | concept | 旧章:204-219; evals/cases/boundary |
| 11 | L1/L2/L3 成熟度分级无对应物 | concept | 旧章:287-304; evals/cases/regression/manual-proactive-line.json; backend/internal/conversation |
| 12 | archify 15 张图源、showcase 不入库 | implemented-at | architecture/README.md:7-40; architecture/archify |
| 13 | 企业导播台独立控制台 | implemented-at | PRODUCT.md:9-11; backend/cmd/server/main.go:408-412; backend/internal/directordraft; backend/internal/operatorwrite |
| 14 | 短期匿名会话身份 | implemented-at | README.md:104; backend/cmd/server/session_api.go; backend/migrations/026_anonymous_device_identities.sql:1-9 |
