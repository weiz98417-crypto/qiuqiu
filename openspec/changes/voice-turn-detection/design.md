# Design: Voice Turn Detection — 说完判定离线评估与决策（2026-09-25）

## 评估方法（任务 1.2 / 修订注预注册判据的执行）

### Harness

纯函数层离线评估，客户端 dart 侧，不碰采集栈：

- 判定对象：`client/lib/services/turn_detector.dart` 的话轮判定降级链（与生产共用同一实现，非独立复刻）。
- 标注用例集：`client/tool/turn_cases.dart`——三类观赛真实场景的确定性合成，三类各 5 例共 **15 例**，每例是「分段 RMS 序列（50ms 帧）+ 期望说完点」的标注：
  - **A 句中停顿**（该等没等的高危场景）：用户停顿思考后继续，如「那个进球……嗯怎么说呢」，中段静默 600–1200ms；期望说完点在第二段语音收口。
  - **B 正常句尾收束**：一次成句自然收尾（含短促重复与能量起伏变体）。
  - **C 环境噪声/长尾拖音**：说完点之后是 continue–start 阈值之间的歧义带能量（拖音/电视伴音/人群欢呼脉冲/背景人声串入，800–2500ms）。
- 参数扫描：静默阈值 **800 / 1000 / 1400（基线）/ 1800ms** 四档 + **语速自适应变体**（基 800ms，话轮内被吸收的犹豫小停顿每段将生效阈值上浮 25%，封顶 2000ms）。
- 指标定义：
  - **提前截断**（该等没等）：判完点早于期望说完点（抢答）。
  - **拖尾**（该断没断）：录音内未决，或轮次延迟 > 静默阈值 + 600ms（余量覆盖帧量化与收尾抖动）。
  - **误判率** = (提前截断 + 拖尾) / 用例数；**轮次延迟** = 说完点 → 判完点（正确例上统计 p50/p90/均值）。
- 运行：`cd client && dart run tool/turn_detection_eval.dart`（下表为 2026-09-25 实跑输出）。

### 数据表（四档 × 三类，实跑）

| 静默档 | A截断/拖尾 | B截断/拖尾 | C截断/拖尾 | 误判率 | 延迟p50 | 延迟p90 | 延迟均值 | 截断平均提前量 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 800ms 固定 | 4/0 | 0/0 | 0/5 | 60.0% | 800ms | 800ms | 800ms | 1350ms |
| 1000ms 固定 | 2/0 | 0/0 | 0/5 | 46.7% | 1000ms | 1000ms | 1000ms | 1350ms |
| 1400ms 固定（基线） | 0/0 | 0/0 | 0/5 | 33.3% | 1400ms | 1400ms | 1400ms | — |
| 1800ms 固定 | 0/0 | 0/0 | 0/5 | 33.3% | 1800ms | 1800ms | 1800ms | — |
| 语速自适应（基 800ms） | 4/0 | 0/0 | 0/5 | 60.0% | 800ms | 1000ms | 867ms | 1350ms |

逐例判完延迟矩阵（说完点→判完点，ms；负值=提前截断量；未决=—）：

| 用例 | 800ms 固定 | 1000ms 固定 | 1400ms 固定（基线） | 1800ms 固定 | 语速自适应（基 800ms） |
| --- | --- | --- | --- | --- | --- |
| A1 那个进球，[600ms]回放里看得很清楚。 | 800 | 1000 | 1400 | 1800 | 1000 |
| A2 这球……[800ms]门将应该要负责的。 | -1200（截） | 1000 | 1400 | 1800 | -1200（截） |
| A3 那个进球……[900ms]嗯怎么说呢，越位在先。 | -1100（截） | 1000 | 1400 | 1800 | -1100（截） |
| A4 我觉得……[1000ms]裁判这次吹得没问题。 | -1800（截） | -1600（截） | 1400 | 1800 | -1800（截） |
| A5 下半场刚开始……[1200ms]对，就是那次反击。 | -1300（截） | -1100（截） | 1400 | 1800 | -1300（截） |
| B1 这波进攻配合打得真漂亮。 | 800 | 1000 | 1400 | 1800 | 800 |
| B2 主教练下半场的换人调整直接改变了比赛节奏。 | 800 | 1000 | 1400 | 1800 | 800 |
| B3 好，进了！ | 800 | 1000 | 1400 | 1800 | 800 |
| B4 这脚……这脚射门太可惜了。 | 800 | 1000 | 1400 | 1800 | 1000 |
| B5 角球开出来，前点一蹭，后点包抄推射入网！（能量起伏） | 800 | 1000 | 1400 | 1800 | 800 |
| C1 说完后 800ms 麦克风拖音尾。 | 1600（拖） | 1800（拖） | 2200（拖） | 2600（拖） | 1600（拖） |
| C2 说完后 1.5s 电视伴音串入。 | 2300（拖） | 2500（拖） | 2900（拖） | 3300（拖） | 2300（拖） |
| C3 说完后 2.5s 观赛环境低鸣长尾。 | 3300（拖） | 3500（拖） | 3900（拖） | — | 3300（拖） |
| C4 说完后人群欢呼：短脉冲高于 start 阈值+歧义带拖尾。 | 2300（拖） | 2500（拖） | 2900（拖） | 3300（拖） | 2300（拖） |
| C5 说完后背景人声串入：歧义带+短促人声+歧义带。 | 2700（拖） | 2900（拖） | 3300（拖） | 3700（拖） | 2700（拖） |

## 对照预注册晋级判据的结论

判据（proposal 修订 2026-09-25）：a) 调参纯 VAD 若标注样本**误判率 >15% 或轮次延迟 p90 >1.2s** → 直接跳 **c) 自部署模型**（首选 LiveKit / pipecat smart-turn 线 ONNX 小模型，本地推理、CPU 可跑）；b) 完形度规则仅作降级链中间档。

| 静默档 | 误判率 >15%？ | p90 >1.2s？ | 判据指向 |
| --- | --- | --- | --- |
| 800ms 固定 | 是（60.0%） | 否（800ms） | c |
| 1000ms 固定 | 是（46.7%） | 否（1000ms） | c |
| 1400ms 固定（基线） | 是（33.3%） | 是（1400ms） | c |
| 1800ms 固定 | 是（33.3%） | 是（1800ms） | c |
| 语速自适应（基 800ms） | 是（60.0%） | 否（1000ms） | c |

**任一扫描档都无法同时满足两条判据，全部指向 c。** 结构性原因（不是调参问题）：

1. **C 类在能量线上无解**：continue 阈值之上的歧义带能量（拖音/电视/欢呼）被现行为当作「语音继续」，静默永不累计——拖尾误判 5/5 恒定存在，构成误判率 33.3% 的下限；任何阈值都治不了，只决定在拖尾上再叠加多少毫秒。
2. **A 类与延迟互斥**：p90 ≤ 1.2s 要求阈值 ≤ 1200ms，而 ≤1000ms 档对 ≥阈值的句中停顿全部抢答（A 类截断 2–4 例，平均提前 1350ms——等价于把用户后半句话抢走，观赛场景最伤）。
3. **语速自适应不是出路**：吸收式自适应只能保护「犹豫之后的收尾」（A1/B4 延迟 800→1000ms），对话轮内**第一段长停顿结构性失效**（截断发生在自适应生效之前），数据与固定 800ms 档完全一致。
4. 真静默的判完延迟 = 阈值本身，能量线在「短延迟」与「停顿容忍」之间只有线性权衡，无第三杠杆；第三杠杆（语义完形度）只能来自文本/模型侧。

### 决策

**选 c) 自部署轮次检测模型**（LiveKit smart-turn / pipecat smart-turn 线 ONNX 小模型，本地推理，与 bge-m3 同 Ollama 自托管纪律，CPU 可跑）。b) 完形度规则按预注册定位仅作降级链中间档，不单独评估。

**本轮实施边界**（按 change 边界「不引入 ONNX/模型依赖」）：只落决策记录、升级触发条件与降级链骨架（模型/规则为预留插槽），模型接入另起实施轮。

## 实施（2026-09-27：决策 c 模型本体接线——LiveKit EOU 多语版 sidecar + 客户端 model 插槽远程化）

### 选型与部署形态

**LiveKit smart-turn 线，`livekit/turn-detector` revision `v0.4.1-intl`（multilingual，zh 在官方支持表）**。ONNX q8 量化（396MB，Qwen2.5-0.5B 级），CPU 实时可跑，与 bge-m3 同自托管纪律。推理逻辑逐行对齐 LiveKit agents `livekit-plugins-turn-detector/base.py`（@1.5.0 与 main 同形）：文本归一（NFKC/小写/去标点）→ 合并相邻同角色 → `apply_chat_template` 去尾部 `<|im_end|>` → tokenizer 左截断编码（max_length=128）→ onnx 前向取末位概率。

- 模型清单（10 文件）：`onnx/model_q8.onnx`（396,316,457B）+ tokenizer 全家桶（tokenizer.json 11.4MB / tokenizer_config / special_tokens_map / added_tokens / vocab / merges / config）+ `languages.json`（官方逐语言校准阈值）+ `ort_config.json`。
- 下载：`scripts/turn-model/download.mjs`（纯 node，registry 无关）——hf-mirror.com 拉取（huggingface.co 不可达，已实测）、Range 断点续传、sha256 校验清单锁定自 HF tree API 的 LFS oid。实测全量 **35.2s**，全部校验通过。落盘仓库外 `E:\tools\turn-model\v0.4.1-intl\`（gitignored，env 可覆盖）。
- sidecar：`backend/cmd/turn-sidecar/`（Python：FastAPI + onnxruntime + transformers(tokenizers)），`POST /turn {text, chatCtx?} → {probability, isComplete}`、`GET /healthz`；端口 8091，模型目录 env `TURN_MODEL_DIR`；推理串行锁（CPU 前向排队，延迟可预期）。compose 新服务 `turn-sidecar` profiles:["turn"]（与 sensevoice-aed 同款隔离纪律，模型 volume 只读挂载，backend 无 depends_on）。

### 实测延迟（396MB q8，CPU，本机 2026-09-27）

端到端 HTTP（含归一+模板+编码+前向）：**p50 24.6ms / p90 34.1ms / max 40ms**（n=20 混合样本）。远低于任务预估的 100-500ms——「提前问 600ms 起查」的 500ms 服务端预算（QIUQIU_TURN_SIDECAR_TIMEOUT_MS）内绰绰有余，结论可在静默阈值帧前赶回。

### 判完阈值：预注册「0.5 起」被实测否决，改用官方逐语言校准值

实测该模型家族的概率量程是 0~1 全幅（en 完整句 0.30-0.86，zh 完整句 0.07-0.47），**0.5 阈值会把一切话轮判「没说完」**（所有否决→话轮挂到上层超时）。`languages.json` 是官方内部评测的逐语言校准（zh：threshold 0.0066，TPR 0.993 / TNR 0.866），sidecar 默认取 `TURN_LANGUAGE`（默认 zh）的校准值，`TURN_COMPLETE_THRESHOLD` 显式覆盖。

### 服务端契约（watchconnection 新 WS case，对齐 duplex_event 写法）

上行 `turn_query {utteranceId, text}` → 服务端只做转发（独立协程 + 500ms 预算 + 熔断 3 次开 10s，在途上限 4）→ 下行 `turn_result {utteranceId, isComplete, probability}`。**降级链闭环的下行语义（grilling 已定）**：`QIUQIU_TURN_SIDECAR_URL` 留空（默认）时服务端对 turn_query 直接回 `turn_result{isComplete:null}`；sidecar 超时/熔断/坏响应同样回 null——客户端 model 插槽收到 null 即下探静默档，**客户端零特殊分支**。转写文本不入日志（与 duplex_event 同纪律）。实现：`backend/cmd/server/turn_relay.go` + `watchconnection.go` 16-case；配置 `backend/internal/config/config.go`（QIUQIU_TURN_SIDECAR_URL / QIUQIU_TURN_SIDECAR_TIMEOUT_MS，默认空/500）。

### 客户端接线（model 插槽的远程版）

- **提前问**（决策点前置到静默 600ms，而非只在阈值帧）：链新增 opt-in 字段 `modelEarlyQueryAfter`（零=保持预注册插槽语义，既有测试/harness 不受影响；VADService.attachTurnModel 注入远程模型时置 600ms），静默累计过门槛即逐帧咨询 model 阶段，true 提前判完（~650-900ms，对比固定档 1400ms）、false 走既有否决语义、null 透传。
- **RemoteTurnModel**（turn_detector.dart，纯 Dart）：turn_query 上行经既有 WS；节流 300ms + 单飞 + 起查门槛双保险；文本源=最近一次 transcript partial（无文本不判）；**结论绑定其判定时的文本**——文本不变期间重复咨询取同一结论（否决持续有效、不重发请求），partial 更新即作废；utteranceId 关联，错话轮迟到结果丢弃；null 结论与发送失败话轮内粘滞（不重询，稳定下探静默档）；在途超时 450ms 只按未决（可再询）。
- **两处配套语义修正**（接线中发现，皆有测试锁定）：
  1. **静默兜底定时器让位否决**：vad_service 的静默定时器原与静默档同阈值同判——model 否决期内会被能量线抢判。现触发时先问 `chain.vetoActive`，否决压制中重新武装定时器（决策 c 的「模型说没说完就不能被能量线兜底抢判」）。
  2. **同文本连续否决上限（2 次）**：RemoteTurnModel 防误判保险——模型对同文本持续 false 时话轮不能无限挂起，超限后本话轮放弃远程判定交回静默档（默认档覆盖约 3.4s 思考停顿，超出评估 A 类 600-1200ms 分布）。
- 客户端不新增设置开关：开关即服务端 URL（留空=链自动退回纯静默档）。

### 真模型冒烟（中文两态，校准阈值档）

| 样本 | probability | isComplete |
| --- | --- | --- |
| 我觉得裁判这次吹得（前缀，没说完） | 0.0000 | false |
| 我觉得裁判这次吹得没问题（说完） | 0.3555 | true |
| 今晚这场的氛围真是（前缀，没说完） | 0.0001 | false |
| 今晚这场的氛围真是没得说（说完） | 0.3601 | true |
| 下半场刚开始对就是（前缀，没说完） | 0.0005 | false |
| 下半场刚开始对就是那次反击（说完） | 0.1369 | true |
| 主教练下半场的换人调整（前缀，没说完） | 0.1625 | **true（误判）** |
| 主教练下半场的换人调整直接改变了比赛节奏（说完） | 0.1734 | true |

对照实验（同管线）：同模型英文对（0.30-0.86 完整 vs 0.0004-0.047 前缀）与 en 版模型（v1.2.2-en）均教科书式两态分离——**管线实现正确，zh 合成观赛片段上模型质量中等**（部分前缀分数与完整句重叠），与官方 TNR 0.866 一致量级。预注册的「中文话轮自评」结论：**zh 可用但弱于 en**，A 类深截断（词组中断）可保护，犹豫式碎片（嗯怎么说呢）有误判；后续以 voice-duplex 1.4 同款真机 telemetry 复核，阈值经 env 调优，zh 不可用则按预注册回退 TEN Turn Detection 自托管。

### 测试与验证（2026-09-27 实测）

- Go：`cmd/server` turn_query 四路单测（未配置回 null / 正常转发 / sidecar 失败回 null / 坏消息丢弃，真实 WS + httptest fake sidecar，对齐 voice_latency_test 风格）+ config 默认值单测；`go test ./...` 30 包全绿。
- Dart：链提前问四路（默认链不受影响 / true 提前判完 / false 压制与过期 / null 下探）+ 持续否决不判完 + RemoteTurnModel 九路（起查/单飞/无文本/结论有效期/错话轮/null 粘滞/发送失败/超时再询/文本作废/否决超限）；`flutter test` 198 例全绿；`dart analyze` 零新增（4 条既有 info 均在未触碰文件）。

### 已知限制

- 评估 harness（turn_detection_eval.dart）与标注用例未注入模型阶段重跑——模型判定依赖 WS/ASR partial 时序，纯帧序列 harness 表达不了（与 evals runner 表达不了 WS 时序同理沿先例）；模型接入的验收改由真机 telemetry 对比承担（抢断/误断率 vs 纯 VAD 基线）。
- 提前问文本源是 ASR partial（非终稿），模型在部分转写上的分布与训练整句有差——冒烟已按 partial 形态（前缀）评测。
- zh 模型质量中等（见上），误判代价不对称：false 否决（模型说没说完）最多损失否决额度后回落静默档；true 误判（提前判完）靠否决额度上限 + maxDuration 兜底，真机数据决定阈值与是否换 TEN 线。

## 实施（任务 1.3：判定处挂降级链，静默阈值参数化）

### 降级链形状

```
说话中，逐帧 RMS → 话轮判定降级链（client/lib/services/turn_detector.dart，纯函数）：
  ① model 阶段（预留插槽，本波未接入）  ── 决策 c 落地时注入 smart-turn ONNX 推理
  ② completenessRule 阶段（预留插槽）   ── 完形度规则中间档
  ③ silence 静默档（实装，链的兜底）    ── 连续真静默达生效阈值即判完
       生效阈值 = strategy: tuned(可配阈值/可开语速自适应) | fixed(固定档=历史行为)
       默认 fixed 1400ms（评估后的过渡档：截断 0，C 类拖尾为已知残留）
插槽语义：静默档即将判完的帧先行咨询；true=采纳，false=否决（再累计满一段
生效阈值后二次询问，不反复刷插槽也不永久挂起），null=未决下探下一环。
```

- VADService 判句处已挂链：逐帧喂 RMS，链判完即 `_finishSentence`；静默兜底 Timer 的时长改取链的**当前生效阈值**（参数化，删除硬编码 `silenceTimeoutMs = 1400`）。
- 配置入口：`VADService.configureTurnDetection(TurnDetectionParams)`——阈值/策略/自适应/回退开关全部参数化；`fallbackEnabled` 关闭后 tuned 档无结论时不再回退固定档。默认参数 = fixed 1400ms，行为与 voice-duplex 落地基线完全一致。
- 评估 harness 与 flutter 测试共用同一实现：`turn_detector_test.dart` 锁能量阈值单源（与 VoiceActivityGate 同值）、链语义（插槽采纳/否决/下探）、以及标注用例结论复现（A3 在 1400ms 档被吸收 / 在 800ms 档被截断、C3 长尾不可判完）。
- Web 端（web_recorder 的 JS 侧判句）不在本轮范围，接入点留待模型轮一并处理。

### 升级触发条件（决策 c 的实施另起条件，已满足①）

1. 本评估判据已触发（误判率/p90 双超标，结构性无解）——模型轮立项依据成立。
2. 实施前置：voice-duplex `duplex_event` telemetry 真机分布核对（抢答/干等占比与 A/C 类形态一致即可按 C 类优先校准模型）。
3. 模型选型约束：ONNX 小模型（BERT 级）、CPU 实时可跑、中文话轮可用（smart-turn 上游以英文为主，中文需自评，不可用则回退 TEN Turn Detection 自托管）。
4. 验收：同一标注用例集复跑（harness 直接可用，把模型阶段注入链即可），误判率与 p90 同时达标签收线；再叠加 H 域 eval 停顿不抢答/噪声不误判例。

### 与 voice-transport-upgrade 的协同

延迟分解与端到端数据同批一次采齐（修订注数据采集并批）：服务端 WS 链路已补结构化延迟日志点（见 transport change design），真机 p90 标注为**待真机会话**，本轮不硬造。

## 已知限制与后续

- 标注用例为确定性合成（分段电平恒定、无真实频谱/混叠），分布按 proposal 记录的失败形态设定；telemetry 真机分布核对是模型轮前置。
- 链假设与 duplex_gate 一致（每个 PCM 块记作一帧 50ms）；平台分帧差异与 duplex 门共用同一假设与校准路径。
- 停顿-截断边界（停顿恰等于阈值）按「判完」计，实测上等价抢答竞态；模型接入后由语义判定消除。
