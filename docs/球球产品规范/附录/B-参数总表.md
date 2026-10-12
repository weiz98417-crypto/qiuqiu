---
篇号: "B"
标题: 附录 B · 参数总表
性质: 附录（规范件——本附录为全库参数唯一口径，各篇参数与之冲突时以此为准；正文章节不设同级参数总表，只保留需求级数值与域级展开）
状态: active
版本: v3.0
基线: a7e5100（2026-06-07）
文体: reference（查表体：按域分组全量登记，每参数五列——名称/值/单位/代码出处/承载篇）
上游: PRD-球球-v3.0.md 第八章 8.3 与第五章 5.1 延迟预算；各篇代码实测硬数字
评测挂钩: 无——本篇是被引用的口径源；各参数的验收断言落承载篇「验收与评测挂钩」节
裁决点: 本表不登记新裁决点；带 ⚖️ 标注的参数行照实悬挂（B1/B4/B12/B17 等），汇总见 37 篇
---

# 附录 B 参数总表

> 本篇基线：2026-06-07 仓库 a7e5100。

**本篇定位**：全库参数唯一口径。立项统一数字表在此全量转正，并合并 PRD 第五章延迟预算与各篇域级参数——去重后按 15 个域分组登记（14 个参数域 + §15 统一口径与约定域）。每参数五列：名称 / 值 / 单位 / 代码出处 / 承载篇。代码出处给到文件（行号锚已在各章核对）；承载篇是该参数的规范展开与执法面。

**冲突裁决规则**：任何篇的参数与本表冲突，按本表改写（00 篇 §6.8 数字唯一口径条款）。本表修订先改代码、再改本表、再同步各篇——顺序不可倒置。

**导读**：§1 语音域｜§2 比赛事实域｜§3 知识域｜§4 记忆域｜§5 关系与情绪域｜§6 对话与表达域｜§7 主动触达域｜§8 协议与连接域｜§9 账号与安全域｜§10 观测与告警域｜§11 部署与环境域｜§12 舞台域（Live2D）｜§13 评测域｜§14 客户端、运营台与治理域｜§15 统一口径与约定域。

---

## 1. 语音域

### 1.1 采集与识别（ASR/VAD）

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| ASR 主路 | MiMo（mimo-v2.5-asr） | — | `backend/internal/asr/client.go`；自托管评估不过门维持（FunASR final CER 0.107 vs MiMo 0.040，d7fc4e5） | 06、20 |
| ASR 熔断/超时 | 3 次 / 10s / 30s | 次·秒 | `asr/client.go:31-39` | 20 |
| ASR 热词 | 支持 `hints` 数组（`asr_options.language=zh`） | — | `asr/client.go` | 20 |
| 采集格式 | PCM 16kHz / 单声道 / 16bit，回声消除+噪声抑制 | — | `client/lib/services/vad_service.dart:13-19` | 06 |
| VAD 起音/继续阈值 | 0.015 / 0.006（≥2 帧确认） | RMS | `vad_service.dart:22-24` | 06 |
| VAD 帧/预滚/兜底 | 50ms 帧 / preRoll 4 帧 / maxDuration 15s | — | `vad_service.dart:165-166` | 06 |
| ASR 分片节奏 | 1.5s 一片 + 0.25s 重叠；整段上限 30s | 秒 | `asr/session.go:11-15` | 06、18 |
| 每连接识别会话上限 | 8（超限 transcript_error 拒绝） | 路 | `cmd/server/transcription.go:20,98` | 06、18 |
| 识别会话降级链 | 流式 ASR → 整段重提（fallbackAudio→user_speech）→ 文字输入 | — | `streaming_transcription.dart`；`watchconnection.go:825` | 06 |

### 1.2 说完判定（smart-turn 三插槽链）

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| sidecar 模型 | LiveKit turn-detector v0.4.1-intl，q8 ONNX，396MB | — | `backend/cmd/turn-sidecar/app.py`（4ed146c） | 06、20 |
| sidecar 推理延迟 | p50 24.6 / p90 34.1 | ms | 4ed146c 实测记录 | 06、20 |
| sidecar 端口/契约 | 8091；`POST /turn`、`GET /healthz` | — | `app.py:37`；`docker-compose.yml:170-184` | 20、23 |
| zh 判定阈值 | ≈0.0066（languages.json 校准；预注册 0.5 被实测否决） | 概率 | `app.py:33-49` | 06 |
| 服务端转发预算/在途上限 | 500ms / 4（饱和回 null 不排队） | ms·条 | `cmd/server/turn_relay.go:31,35` | 18 |
| 客户端提前问/节流/超时/否决 | 600ms / 300ms / 450ms / 同文本 2 次否决弃远程 | ms·次 | `client/lib/services/turn_detector.dart:29,410-429` | 06 |
| 静默过渡档 | 1400ms（+25%/犹豫，封顶 2000ms，minHesitation 100ms） | ms | `turn_detector.dart:96-104`（e9b393b 离线评估决策） | 06 |
| 插槽链 | model → completenessRule（预留）→ silence | — | `turn_detector.dart` | 06 |

### 1.3 合成与投递（TTS）

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 投递形态 | **整段投递**（句粒度流式已拆除：韵律盲测 10 对 2 胜 8 负、80% 嫌弃 vs 判据线 30%，e35c3fe） | — | `tts/stream.go` 保留为已测库；`SynthesizeResponseStream` 出生产路径 | 06、20 |
| 首响接受现实 | 3-7（整段合成+投递累计） | 秒 | e35c3fe 裁决记录；ADR-0012 修订注 | 06、20 |
| TTS 云腿 | mimo-v2.5-tts，音色「冰糖」，整段 WAV | — | `backend/internal/tts/client.go:40-52` | 06 |
| TTS 供给三态 | cloud / local / local_first（cloud 默认=字节级回归锚；local 失败保持并告警；local_first 失败自动回云） | — | `tts/supply.go:23-25`；迁移 055 ops_settings 持久化 | 06、26 |
| TTS 本地腿 | OpenAI 兼容 `/audio/speech`，Fun-CosyVoice3（cosyvoice-v3） | — | `tts/local.go:44` | 06、23 |
| 本地腿健康门控 | 30s 周期试合成「你好。」×连续 2 次过门才亮「本地可选」 | 秒·次 | `tts/local_health.go:16,20` | 26 |
| 情绪指令通道 | Affect State 5 档 × 沟通动作 10 act 指令模板全表（阈值 arousal 0.65 / valence ±0.3 / tension 0.55） | — | `tts/instruction.go`；`cmd/server/tts_style.go` | 10 |
| TTS 熔断 | 3 次 / 10s；流式空闲超时 10s（已测库） | 次·秒 | `tts/client.go`；`tts/stream.go:26,31` | 20 |

### 1.4 伴随反应、双工与唤醒

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| backchannel 事件白名单 | 4 值：big_chance / miss / save / var_check | — | `backend/internal/backchannel/backchannel.go:22-27` | 07 |
| backchannel 限频 | 半场 ≤3 / 全场 ≤6 / 10s 风暴去重 | 次·秒 | `backchannel.go:14-16,98` | 07 |
| backchannel 短语/TTS 预算 | ≤10 字 / 5s（失败即弃，回退纯文字）；紧凑尾巴 ≤12 字 | 字·秒 | `backchannel_audio.go:16`；`instruction.go:18` | 07 |
| duplex 抢断门 | 能量 0.0225（0.015×1.5）/ 时长 400ms / squash 窗 300ms / fired 锁存 | — | `client/lib/services/duplex_gate.dart:14-32` | 06 |
| 误打断自愈/降级 | <4 字且重叠 ≥0.5 恢复播放；连击 3 次降级半双工 | 字·次 | `match_session_controller.dart:199-201` | 06 |
| duplex 惰性恢复退避 | 3min 起步翻倍封顶 30min | 分钟 | `match_session_controller.dart:206-208` | 06 |
| 唤醒词 | 「你好球球」；score 1.8 / threshold 0.3 / cooldown 1.5s / openTimeout 8s（评估门 10/10 唤醒、0 误触） | — | `client/lib/services/wake_service.dart:74-77`；`scripts/wake-eval/results/FINAL-nihao-s1.8-t0.3.json` | 06、12 |
| 唤醒引擎/平台 | sherpa-onnx KWS zipformer wenetspeech 3.3M int8；Android/Windows，web 排除；设置页默认关 | — | `wake_engine_sherpa.dart`；`match_screen.dart:255-271` | 12 |
| 语音旁路置信门 | useraffect 置信 ≥0.55（低落闭集 sad/sigh/low/fearful × 支持队落后改道安慰）；策略偏置开关默认 false | — | `backend/internal/useraffect/client.go`；`config.go:163` | 10 |
| 气氛旁路 | 合法 asr_chunk 原样旁送 AED sidecar（8090），在途上限 4，失败静默；旁证永不作事实 | — | `cmd/server/ambient_relay.go`；`observation/ambient.go` | 10、14 |
| 抢话门真机对照 | 800ms 抢话门浏览器假麦克风环境不可评估，留待真机会话（预采结论） | ms | `openspec/changes/voice-transport-upgrade/design.md`（c5a011c） | 06 |

---

## 2. 比赛事实域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 账本状态机 | 五态 append-only：provisional / confirmed / revoked / conflict / reconciled | — | `matchstate/store.go:27-34`；迁移 013 五态 CHECK | 14、17 |
| 外部来源入账 | 只进 provisional；改判=correction 追加，永不覆写 | — | `fact_ledger.go`（projector v1） | 14 |
| 冲突裁决 | open / resolved，永不自动裁决；人工落点 `conflicts/{id}/resolve` | — | `store.go:54-58`；`conflict_resolution.go` | 14 |
| 确认双通道 | 信任分级自动确认（`auto:stability` 显名）+ 运营人工确认（ADR-0024 修订 ADR-0002） | — | `datasource/stability.go` | 14 |
| 信任分级白名单 | 代码 7 类：goal / red_card / yellow_card / substitution / kickoff / halftime / fulltime（default-deny）；fulltime 行挂 ⚖️B1（与 ADR-0024 正文 6 类及 stability.go 头注矛盾） | — | `stability.go:17-25`；`stability.go:8-9` 头注 | 14 |
| 稳定窗自动确认 | 默认 30s（env `QIUQIU_AUTOCONFIRM_WINDOW_SECONDS`，0=禁用）；sweep 节拍 5s | 秒 | `stability.go:33`；`config.go` | 14 |
| 漂移否决 | 漂移序号制（`driftSeq` 递增，序号落后即重置窗——不依赖时钟，861bfae 根修） | — | `stability.go` `stabilityMark`；`poller.go:211-249` | 14 |
| 数据源四型 | manual / replay / api-sports / espn | — | `datasource/manager.go:16-20`；`espn.go:25` | 14 |
| 赛程源 | 仅 api-sports（ESPN 不供赛程，只供 summary 快照 diff） | — | `main.go:59-95`；`espn.go` | 14 |
| ESPN 轮询节拍 | 默认 5（无 SLA 不快于 5） | 秒 | `manager.go` ManagerConfig.EspnPollInterval | 14 |
| 双源仲裁 | ESPN×api-sports 同类事实不一致挂起冲突、一致互印证 | — | `manager.go` + `espn_test.go`/`manager_test.go` | 14 |
| 一键托管 | `POST /api/matches/{matchId}/host`（API 已有、运营台 UI 未接） | — | `match_operator_api.go:1133`；路由表 :304 | 14、26 |
| 观察管道 | 七态：pending_sync / corroborating / confirmed / contradicted / conflict / expired / superseded；用户主张只作旁证 | — | `observation/coordinator.go:16-24`；迁移 018 七态 CHECK | 14 |
| 观察 certainty 分域 | 用户主张档（claimCertainty）与旁证专档 `ambient_corroboration`（最低权重档）刻意分域；ambient 事件类型 `ambient_*` 前缀与事实类型永不相交 | — | `observation/ambient.go:27,55`；`companion/agent.go:968` | 14 |
| 自动播报事件类型域 | 21 值（迁移 009 默认 automation.eventTypes：kickoff/goal/shot/big_chance/save/miss/foul/yellow_card/red_card/var_check/var_result/goal_cancelled/penalty/penalty_awarded/substitution/injury/tactical_shift/pressure/halftime/fulltime/match_end） | — | 迁移 003/009 | 14 |
| 进球投影六态 | pending / active / cancelled / checkpointed / revoked / unavailable | — | `fact_ledger.go:62-69` | 14 |
| VAR 细分 | 取消结论→goal_cancelled；确认结论→var_result；其余 var_check | — | `poller.go:315-320` | 14 |
| 冲突时钟窗 | 45（`clocksNear(..., 45)`） | 秒 | `store.go:1571` | 14 |
| 事实修订 source_type | 三值：operator / provider / system | — | 迁移 013 CHECK；`store.go` | 14、17 |
| 事件行 status | 三值：active / corrected / deleted | — | 迁移 001 CHECK | 17 |
| 冲突成员角色 | accepted / candidate（边表 left<right CHECK） | — | 迁移 023/024 | 14、17 |
| 事实分级 | F0-F4 五层（PRD FR-FACT-011 需求口径） | — | PRD 第四章节 1 | 14 |
| 比赛时钟 period 六态 | pre_match / first_half / halftime / second_half / extra_time / fulltime（elapsed ≤21600s） | — | 迁移 021 CHECK | 14、17 |
| 比赛生命周期六态 | draft / scheduled / live / finished / cancelled / archived（+转移表） | — | `lifecycle.go:6-13`；迁移 010 | 17 |
| 账本 MCP 出口 | 只读 4 工具；MCP 结果永不进事实账本（三闸锁定） | — | `mcpserve/redline_test.go`；`companion/boundary.go:121-171` | 14 |

---

## 3. 知识域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 条目库规模 | 121（rules 100 + players 21） | 条 | `backend/knowledge/` 实测；ddff504 | 15 |
| 门判门槛 | ≥100（常驻测试守卫，跌破即红；原 50，⚖️B17 挂调整裁决） | 条 | `knowledge/gate_test.go:19-22` | 15 |
| 作答形态 | 非生成式：单条目检索命中→逐字作答（realizer/LLM 不碰）；未命中如实「不知道」 | — | `companion/intent_handlers.go` handleKnowledgeQuestion | 15 |
| 作答置信门槛 | MinConfidence 0.6（低于不出答案） | — | `knowledge.go:19` | 15 |
| 检索 | 双路评分融合取 max（关键词路含 Answer/Quote 正文命中 +0.5；向量路余弦）——「关键词一票短路」已退役（4b8027e） | — | `knowledge.go` Search | 15 |
| 向量余弦阈值 | 默认 0.55（env `QIUQIU_KNOWLEDGE_COS_THRESHOLD`） | — | `knowledge.go:396`；`config.go:149` | 15 |
| embedding | bge-m3 仅嵌 topics（宿主机 Ollama，dim=1024）；缺席自动退关键词单路 | — | `knowledge.go` topicVector；`embedding/embedding.go` | 15 |
| 判罚附句触发白名单 | 6 类：var_check / var_result / goal_cancelled / red_card / penalty / penalty_awarded | — | `companion/knowledge_trigger.go:17-24` | 15 |
| 附句限频 | 同条目 1 次/场、总量 2 次/场；计数容量 256 用户；quiet/用户说话时禁用 | 次 | `knowledge_trigger.go:26-30` | 15 |
| 带 triggers 条目 | 5（judgment-penalty/red-card/offside/handball/var-scope） | 条 | `backend/knowledge/` grep 实测 | 15 |
| 条目生命周期 | DB 化（迁移 053）+ 057 worldinfo 参数学五列（priority/inclusion_group/sticky_turns/cooldown_turns/probability，默认=旧行为）+「待复查」过滤 | — | `053_knowledge_entries.sql`；`057_knowledge_worldinfo.sql` | 15 |
| 条目分类 | rule-* 38/ terms-* 29 / format-* 22 / judgment-* 5 / faq-* 6 / players 21（38+29+22+5+6=100，+21=121） | 条 | `backend/knowledge/` 目录实测（c3） | 15 |
| 实仓条目置信分布 | 0.85-0.95（全部高于 0.6 门槛） | — | `backend/knowledge/` YAML 实测（c3） | 15 |
| 二期扩容 | 200 条登记（不设门） | 条 | ddff504 / knowledge-worldinfo tasks | 15 |

---

## 4. 记忆域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 写入队列容量 | 256（满则丢弃计数，绝不阻塞话轮） | 条 | `memory/queue.go` queueCapacity | 16 |
| backlog 重试 | 30s 指数退避→10m 封顶，最多 10 次后终态 | — | `queue.go` BacklogRetryDelay/failBacklog | 16 |
| 提取重要性门槛 | 0.35（确定性打分，低于不发起提取 LLM） | — | `memory/heuristic.go` MinExtractionImportance | 16 |
| 打分公式 | clamp01(类基+加成) × 阶段权重（类基：进球 0.90/红牌VAR 0.80/黄牌 0.55/promise 0.80/user_fact 0.70/emotional 0.60/其他 0.30；阶段权重 0.90/1.00/1.05/1.10） | — | `heuristic.go` | 16 |
| 召回 | 三路融合：adapter contains（保序）+ pgvector 余弦 + 内容精确腿；向量/内容路有货时 adapter 降半配额，弃权=adapter 独享全量 | — | `queue.go` Queue.Recall | 16 |
| 召回 top-k / 回合预算 | 5 条；recall/portrait 各 300ms/话轮 | 条·ms | `memobase.go`；`memory_observers.go` | 16 |
| 向量路预算 | 检索 200ms / 嵌入 3s / 并发上限 8 | ms | `queue.go` | 16 |
| 时序衰减 τ | 14（`RECALL_DECAY_DAYS`，只重排不丢条） | 天 | `config.go` | 16 |
| 反思 beat | idle 15min（ticker 2min）；重要性阈值 1.5；兜底每 5 个 idle 轮（≤75min）全量；post_match 由终场事件驱动 | — | `cmd/server/main.go` runReflectionBeat；`queue.go` ReflectImportanceThreshold | 16 |
| 反思归因 | 只记用户实际互动过的最近终场（maxUserMatches=8 窗口），没看则空标签 | — | `queue.go` RecentEndedMatchFor（0fc018e） | 16 |
| 画像冲突操作集 | ADD/UPDATE/DELETE/NOOP（LLM 判定，主张账本 ≤16 条/用户）；无 key 时同槽盲收口 | — | `memory/portrait_ops.go` | 16 |
| 写回 dry-run | LLM 审查（temperature 0.1）+ 比分形态主张零 LLM 直接拒（比分红门） | — | `queue.go` LLMWritebackReviewer/claimScoreRedline | 16 |
| 画像按年龄巩固 | 90（`PORTRAIT_DECAY_DAYS`，超窗未续期软封口，UPDATE 即续期） | 天 | `config.go`；迁移 051/052 时间化 | 16 |
| bge-m3 部署 | 宿主机 Ollama `host.docker.internal:11434/v1`，dim=1024；嵌入文本 2000 rune 截断、客户端 2s 超时 | — | `embedding/embedding.go`；`.env.example`（e81c6a3） | 16、23 |
| Thread TTL | 7 天（权威账本）；relationship 存量条目 14 天回退——双轨并存，权威归 Thread | 天 | `memory/threads.go:19`；`relationship/memory.go` | 16 |
| 提取/入账 reason_code | 全集 15 值（queue 七值 + threads 三值 + portrait_ops 五值：accepted/rejected_invalid/rejected_low_importance/backlogged/replayed_from_backlog 等） | — | `queue.go:23-29`；`threads.go:27-29`；`portrait_ops.go:92-96` | 16、17 |
| relationship memory kind | 七值：shared_moment / open_thread / boundary / procedure / ritual / banter_evidence / taste_evidence | — | `relationship/types.go:352-358` | 16、17 |
| 角色设置三域值域 | initiative∈{natural,quiet,active} / analysis_appetite∈{brief,detailed} / banter_level∈{off,light,playful}（DB 无 CHECK，纯代码域） | — | `relationship/character_settings.go:34-36`；迁移 048 | 12、17 |
| 手记生成 | 终场投影+画像口味 ≤3 条；同 user+match 幂等一篇；比分零编造校验不过即降级确定性底稿 | — | `journal/generator.go` validateBody；迁移 056 | 08、16 |
| 记忆删除 | 立碑 privacy_tombstones 下一回合生效；共同瞬间/手记物理删 | — | `privacy/`；迁移 013 | 16、22 |

---

## 5. 关系与情绪域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 晋升阈值 | 初识→熟脸：共同比赛 ≥2 场+证据 ≥2 类；熟脸→球友：≥3 场+≥3 类；球友→老球友：≥6 场+瞬间回忆>0+（分歧或修复至少一项） | 场·类 | `relationship/policy.go` advanceStage | 09 |
| 证据类计数 | familiar 7 类 / watch_buddy 5 类，取非零类数（非条数） | 类 | `policy.go` familiarEvidenceClasses 等 | 09 |
| 调侃范围 | 6 scopes（match_judgment/favorite_team/favorite_player/prediction/watching_habit/real_life，real_life 默认无许可） | 个 | `policy.go` applyUserCues | 09 |
| 五维情绪向量 | valence [-1,1] / arousal [0,1] / tension [0,1] / confidence [0,1] / engagement [0,1]；基线 0/0.2/0.1/0.6/0.4；先衰减后累加逐维 clamp | — | `relationship/affect.go`；`policy.go` updateAffect | 10 |
| 记忆偏置 | 支持队进球 valence +0.2 / 支持球员 +0.15；在全部限制门之后施加（4d25922 修序） | — | `policy.go` applyMemoryBias | 10 |
| 脏话三级门 | none / mild_non_directed（arousal≥0.7 且 familiar+）/ strong_non_directed（arousal≥0.9 且 watch_buddy+）；用户关脏话或修复态一律 none | — | `policy.go` contentPolicyFor | 07 |
| 修复覆盖层 | 3 类触发（banter_boundary/repetition/over_analysis）；修复期预算 2 句/40 字；3 轮 follow-through 计完成 | — | `policy.go` RepairState | 09 |
| 设置粘性覆盖 | 显式设置 > cue 推断 > talkativeness 推导（ADR-0018）；cue 改写后只镜像 AnalysisAppetite | — | `policy.go` applyPolicy；`companion/character_mirror.go` | 12 |
| 用户语音情绪偏置 | 置信 ≥0.55 × 支持队落后 → 安抚改道；开关 `QIUQIU_USER_AFFECT_POLICY_BIAS` 默认 false | — | `relationship/user_affect_policy.go`；`config.go:163` | 10 |
| 话题账本双轨 | Thread 7 天权威 + relationship 条目 14 天回退（数值差异系有意保留） | 天 | `memory/threads.go`；`relationship/memory.go` | 09、16 |

---

## 6. 对话与表达域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 意图注册表 | 13 个命名意图 + unknown；「比赛反应」是主动回合专属别名，不是独立意图 | 个 | `companion/intent_registry.go`（ValidateIntentRegistry 启动+CI 断言） | 07 |
| 路由置信门 | 0.7（fact+置信门五件套一处声明） | — | `intent_registry.go` | 07 |
| 润色时限 | 5s（夹 1-10000ms）；润色失败确定性兜底 | ms | 润色轮实现（polish-round-3） | 07 |
| 表达预算 | 默认 2 句/80 字；analyze 3 句/160 字；repair 2 句/40 字；casual chat 60 字；goal 全档钉 2/80；halftime normal/active 4/200、quiet 2/80 | — | `policy.go`；`relationship/scenario_matrix.go` | 07 |
| ForbiddenClaims | new_score / new_player / new_event / new_penalty_conclusion 恒开 | — | `policy.go` | 07 |
| guard 违禁短语表 | 30 条（治疗腔/关系越界/内部泄漏）+ 硬禁称谓（老公/老婆/主人/奴才）+ 指向性脏话硬禁 | 条 | `companion/companion_vocabulary.go`；`agent.go` validateRealizedText | 07 |
| 反重复冷却 | 近 12 轮球球话轮 phraseHash 命中即拒 | 轮 | `agent.go` recentPhraseHashes | 07 |
| 沟通动作 | 11 个（现网全集，ADR-0005 单一合同） | 个 | `relationship/types.go` SpeechPlan | 07 |
| 回复 source 值域 | 现网服务端实际发出 17 值（conversation/match_reaction/reminder/schedule_lookup/open_thread_recovery/observation_resolution/first_meeting/backchannel/recovered_delivery/delivery_interrupted/client_playback/operator_voice/chosen_silence/cue/fact_revision/session_open/unavailable）；无白名单校验。**「chat」不是现网值** | — | 全仓 grep 实测 | 07、18 |
| 反初见话术 | 初见问候按昵称/主队分派 4 个口语变体，一次投递（TTL 30s） | 个 | `companion/agent.go` firstMeetingGreeting（25bb195） | 07 |
| 记忆进回合 | 措辞织入四入口（闲聊/主动回合/事实衔接语/线程回访）+ 口味偏置；per-turn 缓存恰拉一次 | — | `companion/memory_realization.go`；`turn_memory_cache.go` | 16 |
| 主动回合 reason 字段 | wire 可选 `reason`（{codes,citation}），老客户端零感知（e1a8457）；reason codes 映射用户话术「✦ 为什么找我聊」 | — | `response_delivery.go:20-28`；`replyReason.dart` | 07、18 |
| MCP 工具层 | 静态契约注册表 + 4 只读工具（出站预算 2s、列表 limit 钳 ≤50）；正向消费冻结（ADR-0023） | — | `companion/boundary.go`；`mcpserve/doc.go` | 07、14 |

---

## 7. 主动触达域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 提醒簿提前量 | 30（DefaultLeadMinutes，赛前触达） | 分钟 | `proactive/proactive.go` | 07、26 |
| 提醒过期 | 开球+30min 未送达翻 suppressed，转记忆素材（importance 0.6） | 分钟 | `proactive/beat.go` SweepLoop（1min tick） | 07 |
| 订阅簿上限 | 3 支/用户（store 层设防 TOCTOU）；展开器每日扫未来 14 天赛程 | 支·天 | `proactive/subscription.go`（9911c36） | 07 |
| 复盘邀约 | 终场+15min 投递、终场+2h 过期；自动创建只给实际观赛者（`fulltime_review` 类型） | 分钟 | `proactive/review.go`；`main.go` 事件观察者 | 07 |
| 主动引用码 | 5 种：`open_thread:<id>` / `shared_moment:<eventId>` / `pivotal` / `reminder:<id>` / `subscription:<id>`（非空 citation 才放行） | 种 | `conversation/proactive_gate.go`；ADR-0015 | 07 |
| 主动发言冷却 | 普通事件 90s 默认；active 档 ×0.6（54s）；quiet/normal 保持基线 | 秒 | `policy.go`；`relationship/talkativeness.go` | 07 |
| 关键事件白名单 | 10 类：goal / red_card / penalty / penalty_awarded / var_check / var_result / goal_cancelled / halftime / fulltime / match_end；白名单外事件（犯规、黄牌、换人等）不进主动候选 | 类 | 立法源 FR-PRO-006（服务端装配处常驻）；07 §4.3 体验侧视图 | 07 |
| quiet 双门 | `IsQuiet && !critical && !pivotal` → gate 层与 policy 层同开同关（5c1a6cc 对齐） | — | `proactive_gate.go`；`policy.go` selectTurnActs | 07 |
| 赛点档 | 点球判罚 / 红牌 / 时钟 ≥75' 一球差进球 → `UrgencyPivotal` 插队+`CitationPivotal`；quiet×赛点=放行单句短播（1 句/40 字） | — | `conversation/pivotal.go`；`policy.go` contentPolicyFor | 07 |
| 主动回合 TTL | critical 45s / normal 12s；初见问候 30s；过期不投递 | 秒 | `cmd/server/main.go:1214-1216`；`delivery.go:448` | 07、18 |
| 赛事三节点 | 中场闲聊（确定性比分底稿）/ 失球安慰（订阅队丢球+非 quiet+每场 ≤1 次，FIFO 512）/ 终场复盘邀约 | — | `companion/proactive_nodes.go`（9eed735） | 07 |
| 日级触达硬上限 | 无（现靠各簿节拍+quiet 双门+场景矩阵约束；引入硬上限须先裁决 ⚖️B21） | — | PRD FR-PRO-014 | 07 |
| 生命周期消息 | 召回/休赛期关怀——规划中（未实现） | — | PRD FR-PRO-015 | 07 |

---

## 8. 协议与连接域

### 8.1 消息面与回执枚举

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| /ws/match 消息面 | 上行 20 类 / 下行 19 类（生产发送点口径；行数即对账锚点） | 类 | `watchconnection.go:991-1300` 读循环 20-case；全集对账 | 18 |
| 首条下行 | `match_snapshot`（`welcome` 仅存于在用的 /ws/ops 变体（ops_stream.go:208），/ws/match 不发送——反幻影注记；旧「main.go 死桩」说法已销项） | — | `watchconnection.go:312`；`ops_stream.go:208` | 18 |
| voice_playback 六值 | started / ended / completed / interrupted / skipped / blocked（blocked 为合法别名，双端映射 skipped；completed 为 ended 别名） | — | `main.go:1388-1400`；`playbackResultState` | 18 |
| playback_result 四终态 | completed / interrupted / skipped / failed（无 started；迟到回执只记 client_late） | — | `conversation/delivery.go:130-134` | 18 |
| voice_status 三态 | failed / text_fallback / tts_fallback（**truncated 已死**，不入枚举） | — | `response_delivery.go:63-75` | 18 |
| first_meeting_status 两态 | delivered / skipped（reason=`first_meeting_already_delivered` 幂等静音门） | — | `response_delivery.go:444,449` | 18 |
| turn_result.isComplete | true / false / **null**（null=未配置/超时/熔断/4xx → 客户端插槽下探静默档） | — | `turn_relay.go:176-183` | 18 |
| duplex_event 三事件 | self_interrupt_suspected / duplex_degraded / duplex_recovered（+lipsync 三 reason=客户端健康事件 6 种全集） | — | `watchconnection.go`；`observation/clienthealth.go` | 18 |
| 投递七态 | planned → text_delivered / audio_started → completed / interrupted / skipped / failed | — | `conversation/delivery.go:13-19` | 17、18 |
| reconnecting | **不是 wire 消息**——客户端本地合成事件（websocket_service.dart:181），服务端零收发 | — | 对账注记 | 18 |
| /ws/ops 下行 | kind 9 类（turn_planned/delivery/signal/fact_revision/media_delivery/playback_result/turn_stale/backchannel/character_setting）；wire 9 字段无正文（结构体级裁剪）；首条 `welcome`（`stream:"ops"`）——全库唯一现役 welcome | — | `ops_stream.go:26-36,208` | 18、26 |
| 互动账本 kind | **九类**（原七类 + backchannel + character_setting；DB 无 CHECK，纯代码域 `ledger.go:19-28`） | 类 | 代码域实测 | 17 |
| playback_state 六值 | started / ended / completed / interrupted / skipped / blocked（035 索引内嵌全集） | — | `ledger.go:159-162` | 17 |

### 8.2 连接保护与传输

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 每 IP 连接上限 | 5（超出 429） | 条 | `config.go:225` | 18 |
| WS 单帧读上限 | 1MB（≈15 秒 16kHz 单声道语音），超限断连 | MB | `config.go:214` | 18 |
| 读写超时 | 60s / 10s | 秒 | `config.go:217-223` | 18 |
| 心跳三拍 | 服务端协议 ping 30s / 客户端应用 ping 25s / 会话看守重验 30s | 秒 | `ws/handler.go:237`；`websocket_service.dart:161`；serveHeartbeat | 18 |
| 客户端自动重连 | ≤5 次；退避 2^(n-1) 秒钳 1-8s；令牌先行刷新；代际防串台 | 次·秒 | `websocket_service.dart:59,178` | 18 |
| 语音补发队列 | ≤4 条 FIFO，超限淘汰最旧；已取消绝不补发；串行发一条确认一条 | 条 | `match_session_controller.dart:193,787-793` | 18 |
| 服务端去重 | signalDeduper TTL 15min 容量 4096；连接级事件 LRU 512；客户端 4 类 Deduplicator 各 256 | — | `signal_deduper.go:10-11` | 18 |
| 请求体上限 | 用户面会话/资料族 16KB；运营写 1MB；WS 单帧 1MB | KB·MB | 18 篇 §1.3（MaxBytesReader） | 18 |
| deliveryKey | 五种键格式族：event / trace / reminder / backchannel / observation；比赛事件格式 `事件ID:factRevision:factStatus` | — | `deliverykey.go:8-13,37-39`；`store.go:198` | 18 |

### 8.3 HTTP 面与迁移

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 控制面端点 | 51 = console 19 + match 运营面 28 + knowledge 4（ServeMux 三表声明式路由） | 个 | `router.go`；`console_api.go:158-184`；`match_operator_api.go:294-322`；`knowledge_api.go:71-74` | 18、26 |
| 用户面族 | sessions（anonymous/login/refresh/revoke）+ me（privacy/export/data/character/portrait/threads/moments/journal 含 album、like）+ matches/catalog + health——表外族，不计入 51 | — | `main.go:649-668` | 18 |
| 错误码约定 | 已知路径错方法 405（带 Allow）；尾部斜杠 404；条件冲突 409；已删 410；异步受理 202；能力缺存储 501 | — | 18 篇 §2.6 | 18 |
| 迁移文件 | 59 个，最高 057；缺 025 号；010-012 同号双文件；**引用一律按文件名**，不写「第 N 个迁移」 | 个 | `backend/migrations/` 实测（矩阵实测） | 23 |
| /ws/ops 队列 | 256，饱和丢弃只计数（不规则化） | 条 | `ops_stream.go` | 26 |

---

## 9. 账号与安全域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 身份口径 | 匿名是默认形态，登录是原地绑凭证的可选升级，登出≠失忆（ADR-0020） | — | `auth/session.go`；CONTEXT.md 词条 | 12、22 |
| 登录凭证 | 邮箱/用户名（3-32 位）+密码 ≥7 位；argon2id 存储（32MiB / t=2 / p=1，迁移 049） | — | `session_api.go:40`；迁移 049 | 22 |
| 终端用户令牌 | access 15min / refresh 30 天（HMAC 签名）；刷新令牌 `<sessionID>.<secret>` 自识别，重用=吊销整条会话链 | — | `auth/session.go:26-28,116-229` | 22 |
| 运营台认证 | JWT（HS256 15min）+ 刷新令牌 30 天单次轮换；PBKDF2-SHA256 100k 迭代；首登强制改密；失败登录写审计（v1 无锁定） | — | `consoleauth/pbkdf2.go:31`；`jwt.go:23`；`refresh.go:12` | 26、22 |
| 账号种子 | 默认关（`QIUQIU_DEMO_ACCOUNTS=1` + development + 空表三条件） | — | `demo_accounts.go`（3c65a3f） | 26 |
| scope 体系 | 4 个 scope 常量（`operator:trace:read` / `operator:match:write` / `operator:fact:confirm` / `operator:fact:correct`）+ auditor 只读角色；用户侧 `user:chat`/`user:read` | 个 | `auth/session.go`；`operatorauth` | 18、26 |
| 运营写幂等 | `Idempotency-Key` 头 + `executeOperatorWrite` 幂等包裹（同键重放返回原结果）；写端点 18 个，其中 2 类走草稿制 | — | `operator_write_api.go:46`；迁移 017 | 26 |
| 审计预算 | 3s 独立预算写 operator_audit，失败仅记日志不回滚不拖响应 | 秒 | `console_api.go:245`；`operatorauth/postgres.go:146` | 26 |
| 隐私 API | /api/me/export（11 类对象族）+ DELETE /api/me/data（异步 202 `{jobId,status}`）+ GET /api/me/privacy | — | `privacy_api.go`；`privacy/service.go:26-40` | 22 |
| 删除生命周期 | 删除期 companion 写路径被挡（横切 enforcement）；6 小时清理；立碑下一回合生效；保留期默认 30 天（`PrivacyRetentionDays`） | — | `companion/privacy_integration_test.go`；`main.go:188,889` | 22 |
| 匿名身份持久 | deviceId 迁 flutter_secure_storage；被冻结 deviceId（删除 pending/failed）请求匿名会话 409 | — | `preferences_service.dart:30-73`；`session.go` | 12、22 |
| 观测日志脱敏 | 情绪信号日志只记 label/confidence/accepted/dropped（不打用户标识）；观测三面无正文 | — | `useraffect_relay.go:161`（5c1a6cc） | 22、26 |
| 无障碍基线 | 语音/文字双通道等价已实现；对比度/字体缩放/读屏/专节规划中 | — | PRD FR-UI-014 | 12 |

---

## 10. 观测与告警域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| Grafana 形态 | as code：6 面板 + 4 告警规则；本机 127.0.0.1:3300，**不在 compose 内** | — | `deploy/grafana/`；c5 §1.3 | 28 |
| 告警判断值 | 语音延迟 P95 >3000ms / 首响 P95 >1800ms（tts_first_audio 锚点随句粒度拆除休眠、规则保留）/ 供应商错误 >3 次/30min / poller 停摆 >600s（历史宽松值 5000/2500/5/900 已废；title 文案残留旧值，以判断值为准） | ms·次·秒 | `deploy/grafana/provisioning/alerting/rules.yml:110,195,232,319`（8f5df3f） | 28 |
| 通知通道 | 钉钉 contact point + webhook；2026-06-03 值班演练全链闭合（20 条真故障 trace→firing→重发→resolved） | — | b1a480b；`docs/design/oncall-drill-20261003.md` | 27 |
| 延迟分解锚点 | 5 段结构化日志：speech_received → asr_finish → asr_final → turn_decided → audio_delivered（+text_delivered/first_audio），锚点表满 64 整表重置；事后 attach 键级合并进 trace | — | `watchconnection.go:152-199`；a46d00c | 20 |
| 端到端发布闸门 | P95 < 3000ms（latency-test 五场景：进球/黄牌/半场/射门/开场） | ms | `backend/cmd/latency-test/main.go`；Grafana 判值同口径 | 25 |
| 客户端健康观测 | ClientHealthLedger 进程内环形缓冲（重启清零）；wire 裁剪至 kind/at/matchId，无身份无正文 | — | `observation/clienthealth.go`；`console_api.go:161` | 26 |
| 直播监听页参数 | 列表上限 200 行；断线重连退避 5s 起指数封顶 30s；暂停=入列冻结（暂停期事件丢弃不缓存）；按 traceId 一键回合回放 | 行·秒 | 18 篇 §3.6（console 直播监听页） | 26 |
| trace 观测 | GenAI 语义对齐（工具名常量单一源+守卫）；语音分段与轮次结论 attach 落 PG | — | 17b337f；`docs/design/trace-genai-alignment.md` | 26 |
| 比赛页轮询/判活 | 轮询 3s / 判活夹 6-20s（需求级口径，PRD 5.1 转记） | 秒 | PRD 5.1.1 | 11、20 |

---

## 11. 部署与环境域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| compose 常驻 | 6 服务：backend / postgres16+pgvector / redis / memobase / memobase-postgres / memobase-redis | 个 | `docker-compose.yml` | 23 |
| profile sidecar | 3 个：turn（8091）/ voice-input（8092，SenseVoice 情绪）/ sensevoice-aed（8090，气氛） | 个 | `docker-compose.yml` profiles | 23 |
| 后端端口 | 8080 默认（`PORT`）；console vite 代理目标 18080——本地 8080/18080 断裂为已知缺陷（⚖️B11，按可跑通路径写+脚注） | — | `config.go:119`；⚖️B11 | 23 |
| Grafana | 本机运行（Windows 解压 E:\tools\grafana，绑 127.0.0.1:3300，匿名 Viewer，/grafana 子路径）；不进 compose | — | `deploy/grafana/README.md`；c5 §1.3 | 23 |
| PG 约束 | PG16 portable 为演示机约束；备份 pg_dump -Fc cron 每日 03:30 | — | `deploy/README.md` | 23 |
| Memobase | ghcr.io/memodb-io/memobase:latest，端口 8019:8000；提取 LLM 复用 MIMO 凭证；事件嵌入开关默认 false | — | `docker-compose.yml`；`.env.example` | 16、23 |
| Live2D 资产 | web 同源 `/live2d-assets/`（缓存头）；Android 应用内 loopback `http://127.0.0.1:<随机端口>`（1d572ac；旧 10.0.2.2:8081 与 qiuqiu://asset 方案已废弃） | — | `client/lib/services/live2d_asset_server.dart:21,43` | 11、23 |

---

## 12. 舞台域（Live2D）

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 渲染引擎 | pixi.js-legacy 7.4.3 + pixi-live2d-display-advanced 1.1.0 + esbuild 0.25.12（可复现构建，输出 sha256） | — | `scripts/live2d-bundle/package.json`；a0011ab | 10 |
| 动作白名单 | **23 个接受值**＝12 组 + 9 变体 + 2 legacy 合成名（cheer/focus）；动作别名 6 条（hold/settle/slump/nod/listening/confused）。「17 类动作」仅是映射表 motions 节设计口径 | 个 | `presentation_vocabulary.go:53-78` ≡ `presentation_state.dart`（三向锁+资产断言） | 10 |
| 表情白名单 | 13 个 + legacy 别名 3 条（low→sad / tense→nervous / deflated→sad） | 个 | `presentation_vocabulary.go:37-51` | 10 |
| 语音风格白名单 | 8 值：natural / quiet / warm / excited / tense / low_disappointed / calm / soft（**energetic 不在白名单**——v2 示例越界教训） | 个 | `presentation_vocabulary.go:82-91` ≡ dart:165-174 | 10 |
| returnMode | 4 值：decay_to_focus / decay_to_listening / decay_to_idle / watching | 个 | `presentation_state.dart:176-181`；`presentation_table.go` | 10 |
| acts 表 | 8 个（动作-意图-情绪三向一致性锁） | 个 | `presentation-map.json` 实测；`presentation_table_test.go` | 10 |
| presentation 事件 | 9 个（事件行赢过一切 act 行；旧「5 类比赛信号」作废） | 个 | `presentation_table_test.go`（事件行 9 项） | 10 |
| 保持窗 | HoldMS 默认 1800ms；静默保持档（quiet hold）3200ms；holdLastFrame 地板 7400ms（`holdLastFrameHoldFloorMS`） | ms | `presentation_table.go`；10 篇 EMO-34/37 | 10 |
| 待机三档 | 低落档（deflated）/ 平静档（calm）/ 亢奋档（energetic）；首挑不亢奋 | — | `idle_tier_picker`（Dart 单测七例） | 10 |
| 口型同步 | 三级退化（vendor_unavailable / decode_failed / jitter_fallback 三 reason 遥测）；advanced 内置口型钉死关闭，wLipSync 1.3.1 承担 | — | `lipsync_health_reporter.dart`；5de8d13；`assets/live2d/README.md` | 10 |
| 废弃标识 | `digital_match_friend` 工程标识全仓无代码，已废弃（F7），不进入本版 | — | 全仓 grep 实测 | 10 |
| 像素回归 | `scripts/live2d-bundle/regression.mjs` 逐动作截图+透明塌底检测；产物入库 artifacts/live2d-regression/ | — | 10 篇评测挂钩 | 10、25 |

---

## 13. 评测域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 套件与用例 | 12 套件 249 用例文件（identity 为空悬套件：枚举有、目录无）；合法枚举 13 个 | 个 | `evals/cases/` 12 目录逐目录实测；`load.go:17-22` | 24 |
| 聚合口径 | offline 聚合 234 实跑（=249−真网档 router_net 15）；2026-05-30 实证档 offline.json 234/234 全绿（gitRevision 6904033）——唯一有实证的全绿档；「248 全绿」从未有实证 | 例 | `load.go:144-157`；`artifacts/evals/offline.json` | 24 |
| 真网档 | router_net 15 例显式点名（`-suite router_net`），不进 all（真 key 费用/抖动） | 例 | `load.go:146-156` | 24 |
| 结构校验脚本 | 5 个 check-*.mjs（presentation-map / director-event-model / console-transport / director-draft-form / live2d-idle-yield），任一失败即中断 | 个 | `scripts/evals/run.mjs` | 25 |
| schema 版本 | 「2026-03」（const 强制；改版=破坏性变更） | — | `evals/schema/eval-case.schema.json:7` | 24 |
| CI 双 job | pr-evals（20min）+ promptfoo-gate（15min，CI 无模型 key）；nightly 档**无 cron**，只能本地手跑 | — | `.github/workflows/evals.yml` | 25 |
| promptfoo 门禁层 | 4 用例 6 断言（javascript×5+equals×1），2 provider，红阻断（注错验红 exit=100） | — | `tests/promptfoo/promptfooconfig.gate.yaml` | 24 |
| promptfoo 观测层 | 4 用例 llm-rubric judge + defaultTest 2 断言（口吻+长度 ≤200），永不挡门 | — | `tests/promptfoo/promptfooconfig.yaml` | 24 |
| 金样本 | 13 份逐字锁 console 契约（console_golden_test.go 消费） | 份 | `cmd/server/testdata/goldens/` | 25 |
| 界面自动化 | Playwright 20 spec / 99 tests / 5,904 行（旧「18 条旅程/5517 行」作废） | — | `tests/evals/` 实测（实测） | 25 |
| 四档阶梯 | offline / pr / nightly / release（`scripts/evals/run.mjs` 单一编排入口；release 不清四键+MiMo 语音冒烟） | — | `package.json:6-9`；`run.mjs` | 25 |
| 门禁包数 | 后端 34 包 go test 全绿 / client 241 tests / console 23 tests | 包 | `docs/handoff-20261007.md` | 25 |
| ASR 对照门 | 16 例四类；FunASR seaco 0.107 / 无热词 0.120 vs MiMo 0.040——不过门，维持 MiMo | CER | `scripts/asr-eval/`（d7fc4e5） | 24 |
| UTMOSv2 | MOS 1-5 排序，进盲测集阈值默认 3.5；**未实跑**（待有卡机器）；中文域只粗筛不下结论 | 分 | `scripts/utmos/mos_rank.py` | 24 |
| 韵律盲测 | 已实跑并触发裁决：句粒度 2 胜 8 负（80% 嫌弃）vs 预注册线 30% → 拆除句粒度（e35c3fe） | — | e35c3fe；ADR-0012 修订注 | 24 |
| smart-turn 对比 | 三件工具已落地（采集 199 条提示语三场景 / 标注复标一致性 ≥90% / 双模态双腿对比）；用户录音标注未开始（约 2h 成本） | — | `client/tool/turn_*.html|mjs`（ed5b8df） | 24 |
| 解说评测集 | ≥50 样本门槛未启动；三硬门（球员实体/比分演进/幻觉）零容忍；BERTScore ≥0.85 硬门全过后才有意义 | — | `docs/evals/commentary-eval-set.md` | 24 |

---

## 14. 客户端、运营台与治理域

| 参数 | 值 | 单位 | 代码出处 | 承载篇 |
| --- | --- | --- | --- | --- |
| 客户端页面 | 10 页（含登录页、手记页） | 页 | `client/lib/screens/` 实测（c4） | 11 |
| 设置页 | 11 节（含抢话打断/唤醒词/气氛感知说明行/账号与同步） | 节 | `settings_screen.dart`（453 行） | 12 |
| web 形态 | PWA（standalone 可安装）；Android 走应用内 loopback 随机端口 | — | c4 §1 | 11 |
| 连续对话 | 默认开启（`continuousConversation ?? true`）；「默认按住说话」类表述违规 | — | `preferences_service.dart:174` | 12 |
| 话痨默认档 | 标准（normal）；未知值回落 normal；「默认安静」类表述违规 | — | `NormalizeTalkativeness` | 07、12 |
| 运营台技术栈 | React 19 + antd 6 + Vite 6 + vitest 5；10 页面九格；侧栏 NAV 7 项+独立实战导演入口 | — | `console/` 实测（c5） | 26 |
| 深链直进 | autoEnter 已实现（深链直进陪看）；「需显式确认」降级规划（⚖️B3） | — | c4；PRD FR-UI-009 | 11 |
| 术语正典 | CONTEXT.md 36 词条（附录 A 从 30 补齐并镜像） | 个 | `CONTEXT.md` | 附录 A |
| ADR | 24 篇（0001-0024；全量重索引+superseded 链补全，⚖️B6） | 篇 | `docs/adr/` | 36 |
| openspec | 活跃 80 + 归档 34 个 change | 个 | `openspec/changes/` 实测 | 00 |
| CHANGELOG | 停在 0.2.0（2026-05-20）；0.3.0 九组待补为工程侧任务，文档不背账（⚖️B5） | — | `CHANGELOG.md`；`VERSION`=0.2.0.0 | 02 |
| 合同口径 | API 合同扩至现网全集（控制面 51+用户面族+login+WS 20/19），全集以路由表为准（⚖️B12） | — | 18 篇 | 18 |

---

## 15. 统一口径与约定域（全库叙事口径）

以下六条不是单点参数，而是跨篇叙事口径；任何篇与本表冲突按本表改写。

| # | 口径 | 内容 | 承载篇 |
| --- | --- | --- | --- |
| 1 | 事故分级 | 全库统一 **S0=最严重 → S3**；旧「S3=最重」反义改写；卷首「一~四级」与 P0-P3 工单口径的映射说明在 27 篇 | 27 |
| 2 | 默认档位 | 话痨三档默认「标准」（quiet/normal/active 中 normal）；连续对话默认开启（`continuousConversation ?? true`）；「默认安静」「默认按住说话」类表述一律违规 | 07、12 |
| 3 | 通知口径 | 应用内三簿（提醒簿/订阅簿/复盘邀约）已运行=现网唯一承诺触达通道；「无系统通知栏推送」仍为真——两者分开表述，不得混写 | 07、11 |
| 4 | 记忆口径 | 自动跨场记忆默认开启、事后治理（删除/立碑/导出）为现网；「逐项记忆许可」「记忆可暂停」不存在，属规划（⚖️B4） | 08、16 |
| 5 | 确认口径 | 事实确认=「信任分级自动确认 + 运营人工」双通道（ADR-0024 立法）；「导播台人工确认是唯一入口」废弃 | 14、26 |
| 6 | 身份口径 | 匿名是默认形态，登录是原地绑凭证的可选升级，登出≠失忆（ADR-0020）；「无注册/匿名是完整方案」类宣言全库清除 | 12、22 |

---

## 设计说明（写法依据，非参数）

1. **转正来源**：本表主体是立项统一数字表的逐行转正（22 行→13 域展开），并合并 PRD 8.3 同源表、5.1 延迟预算与各篇代码实测硬数字；同源多方，冲突时以本表为准并回修源表。
2. **单位列的口径**：无量纲参数（枚举、布尔、形态）单位列写「—」；复合值（如「3 次 / 10s」）在值列完整给出、单位列写主单位。
3. **悬挂参数**：带 ⚖️ 的参数行（B1 信任分级、B4 干预第三档、B11 端口、B12 合同、B17 门判门槛、B21 触达上限）照代码现值登记、裁决状态照实标注——裁决改变时只改本表与受影响篇。
4. **反幻影参数**：本表不收录任何无代码承载的「参数」（逐项记忆许可、日级触达硬上限、生命周期消息等仅以「规划中（未实现）」行出现，不给值）。
5. **与各篇的分工**：本表登记值与出处；参数的行为语义、反例与验收断言归承载篇。承载篇复制本表值属可容忍冗余，但冲突时以本表为准（00 篇 §6.8）。

---

## 修订记录

| 版本 | 日期 | 依据 | 变更摘要 |
| --- | --- | --- | --- |
| v3.0 | 2026-06-08 | PRD-球球-v3.0.md 第八章 8.3；各篇代码实测 | 初版。全量转正统一数字表并合并 PRD 5.1 延迟预算与各篇域级参数，15 域分组登记 |
| v3.0.1 | 2026-06-09 | 复核修复（元数据缺项） | YAML 补「篇号」字段（取值附录字母，00 篇 §6.3） |
| v3.0.2 | 2026-06-09 | 复核修复 | 复核修复：§3 条目分类计数勘误（terms 30→29、format 17→22，补 rule 38；合计仍 121=100+21，与 15 篇 §1.1 分族表对齐）；§8.1 首条下行行 welcome 注记销项更新（旧「main.go 死桩」已不存在——welcome 仅存于在用的 /ws/ops 变体，/ws/match 不发送）〔原误记 v3.0.1 与上行重号，2026-06-09 复评残留清零改号顺延〕 |
| v3.0.3 | 2026-06-09 | 主编通读修订（E1；读者报告 A/B/C 旧编号断链清单） | 「承载篇」列旧篇号清零迁移至 00-39 新号体系（230 行，全列机械映射；语义抽核：语音→06/20、事实→14/17、记忆→16/22、协议→18、部署→23、评测→24/25、观测→26/27/28、ADR→36）；§14 ADR 行「10 篇」计数化石改「24 篇」（与 0001-0024 及 36 篇口径一致）〔原误记 v3.0.2，随上行情改号顺延〕 |
| v3.0.4 | 2026-06-09 | 主编通读修订（E2；读者报告 A/B/C 冗余清单） | §7 主动触达域增「关键事件白名单」行（10 类逐字，立法源 FR-PRO-006）——白名单清单唯一出处落位本表，07 篇 §4.3 逐字列举同步改指针（话痨频控体系四数字 90s/×0.6/45s/12s 的唯一口径地位本表已在位，本轮核验确认）〔原误记 v3.0.3，随上行情改号顺延〕 |
| v3.0.5 | 2026-06-09 | 复评残留清零（R；读者报告 C #4 术语扫尾） | §12 保持窗行「安静请求 3200ms」改「静默保持档（quiet hold）3200ms」——与 10 篇 v3.0.1 术语统一同批；修订记录 v3.0.1 重号消解（第二条起顺延为 v3.0.2-v3.0.4，各行内保留原号注记） |
