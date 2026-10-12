# Live2D 表演、动作与前端事件

> **文档性质**：真实工程叙事。这一篇记录"球球"的 Live2D 舞台是怎样建成的：一个表情动作如何被选出来、如何被约束住、如何在说话与沉默之间交接身体，以及我们用什么测试保证"像在反应"这件事不会退化成随机抽卡。文中所有机制均以仓库现网实现为准，关键路径逐处标注代码位置。

---

## 1. 问题：表情动作怎么做到"像在反应"，而不是随机抽卡

给数字人配一套 Live2D 模型很容易——模型带表情文件和动作组，接上播放器就能动。难的是让动作**有理由**。用户对"像在反应"极其敏感：进球了庆祝、VAR 回看时紧张、被怼了一句委屈地抱怨，这些如果做对了，角色就"活"了；如果做错了——进球时眨眨眼、闲聊时突然蹦一段庆祝——角色立刻退化成一个随机抽卡的播放器，比不动还假。

早期版本的球球就卡在这里。我们踩过的两个坑很有代表性：

- **空表情文件**。模型自带 7 个表情文件，其中 1 号文件其实是空的（不含任何参数绑定），而"思考"表情恰好绑在它上面——每次球球陷入沉思，脸上什么都没发生。一次表演审计把这个问题翻了出来，也促成了后来"表情绑定必须对资产本身做测试"的规矩。
- **图层打架**。动作播放没有所有权规则：说话的收尾、待机的轮换、表演的保持窗各自为政，互相抢身体。用户看到的是三个图层在打架，而不是一个角色在表达。

从这些坑里我们提炼出三条设计立场，后来整个舞台都建立在它们上面：

1. **一张映射表管一切**。什么信号配什么表情动作，写在一份 JSON 里，作为唯一事实源，前后端各自镜像并互相锁死。没有散落在代码里的 if-else 情绪判断。
2. **非法输入落待机**。白名单之外的任何表情动作名，不猜测、不就近替代，直接落回待机。演错永远比不演糟糕。
3. **身体同一时刻只有一个主人**。说话、表演保持、待机轮换之间有明确的交接规则，并且有一个检查脚本盯着这条规则不被后来的改动改坏。

还有一个校准认知的前提：我们说的"反应"，输入从来不是"发生了什么"，而是"发生了什么、用户此刻允许知道什么、关系此刻处在什么状态"这三件事的交集。同一个进球，对看直播的用户和正在防剧透延迟里的用户，不该有同一种演法。表演层的输入天然是被裁决过的，它自己不做事实判断——这是它能保持简单的根本原因。

## 2. 真实设计：一张映射表，两套镜像，三层约束

一条表演信号从产生到落到模型身上，穿过的是这样一条流水线：

```text
比赛信号 / 用户回合
    → 关系层裁决（行为 acts、情感向量 valence/arousal、信号幂等）
    → presentation_table.go 路由（事件行 → 行为行 → 保底行 → 观赛默认）
    → 生成 PresentationPlan（表情/动作/语气/能量/语速/holdMs/回返模式）
    → 服务端白名单归一（presentation_vocabulary.go）
    → WebSocket 下发 → 客户端 presentation_state.dart 再校验
    → Live2D 页面换装 + 保持窗武装 → 窗满按回返模式交接 → 待机三档接管
非法名字在客户端一端被拒绝，落待机，不渲染。
```

下面按这段流水线的顺序展开。

### 2.1 presentation-map.json：唯一事实源

`client/assets/live2d/models/qiuqiu/presentation-map.json` 是整个表演系统的中心。它把模型的全部表现力组织成五个段落：

| 段落 | 规模 | 回答的问题 |
| --- | --- | --- |
| expressions | 13 个表情 → 文件索引 | 这张脸用模型的哪个表情文件渲染 |
| motions | 17 个动作 → 12 组变体 | 这个动作名对应模型里哪个动作组的哪个变体 |
| acts | 8 种沟通行为 | 一次回话"是什么性质"，该配什么表演 |
| events | 5 类比赛信号 | 场上发生了什么，第一反应是什么 |
| phases | 6 个对话阶段 | 此刻轮到谁说话，身体该摆什么姿势 |

几个值得展开的细节：

- **expressions 与空文件禁令**。模型自带 7 个表情文件，但 1 号是空的，所以映射表只允许中性身体状态（idle / focus / listening）绑到索引 0——这条规则源自上面那次表演审计，并由后端测试 `TestPresentationMapNeverBindsNonNeutralNamesToEmptyExpressionFile` 对着资产文件本身锁定，"绑了等于没绑"的表情从此不可能再混进来。
- **motions 覆盖全部 12 组**：hello、idle、listen、speak、think、celebrate、miss、complain、analysis、tense、agree、wave，外加 idle/listen/speak/celebrate 组内的具体变体（idle_01 到 celebrate_02）。宽白名单是刻意为之——早期白名单只有 7 组，导致服务端想表达的动作大半落不下来。
- **acts 是"像在反应"的核心**。同样是回话，性质不同表演就不同：

| 行为 | 象限/档位 | 表演 | 附带效果 |
| --- | --- | --- | --- |
| ActReact 回应 | 正向情绪 | excited / celebrate | — |
| ActReact 回应 | 负向情绪 | nervous / complain | — |
| ActReact 回应 | 平静 | chat / speak | — |
| ActAnalyze 战术分析 | 任意 | thinking / analysis | — |
| ActRecall 回忆共同时刻 | 任意 | happy / agree | — |
| ActRepair 修复关系 | 任意 | sad / agree | 语音能量降 0.3（道歉不掷地有声） |
| ActDisagree 反驳 | 温和 | nervous / complain | — |
| ActDisagree 反驳 | 强烈 | angry / complain | — |
| ActAsk / ActTease / ActAcknowledge | 任意 | thinking·think / tease·speak / chat·speak | — |

- **events 是比赛信号的第一反应**：进球 excited/celebrate，进球被取消 surprised/complain，VAR 改判 surprised/confused，VAR 审查中 tense/tense，射门偏出 sad/miss。其中 goal_cancelled 与 var_overturn 两个条目复活了模型里此前从未被播过的 surprised 槽位——映射表建完之后我们盘点了"哪些表情动作从未出场"，把死槽位逐个接上了触发源。
- **phases 是对话阶段的身体基线**：用户说话时球球摆 listening/listen_01 的听姿，理解中摆 thinking/think，球球开口时切 chat/speak_01 的说姿，开场 happy/hello，终场 happy/wave 挥手告别。idle 一项写的是特殊标记 `affect-idle-tier`——待机身体不归这张表管，归 2.4 节的三档轮换器管。另有一条 delivery 段：被打断的回合落 confused/listening。

### 2.2 服务端白名单与别名归一：名字先过安检

映射表只解决"选哪个"，但服务端生成的每个计划里的名字都必须可信。这里我们做了双向镜像：

- **客户端**（`client/lib/services/presentation_state.dart`）：维护允许的表情集合（13 个）、允许的动作集合（12 组 + 变体 + 少量历史名）、别名表、8 个合法语气风格与 4 个合法回返模式。`fromReplyData` 对收到的每个字段做归一与校验，任何一个名字不合法，整个表演对象直接返回 null——UI 落回待机，不渲染。
- **服务端**（`backend/internal/relationship/presentation_vocabulary.go`）：用 Go 逐字镜像同一份白名单与别名表，服务端在下发前就完成同样的归一，确保到达客户端的名字已经被洗过一遍。
- **映射路由**（`backend/internal/relationship/presentation_table.go`）：把 JSON 的 events 和 acts 段落镜像成一张 Go 路由表，解析优先级固定为四步——比赛事件行优先；否则按声明顺序匹配第一行的行为行（更具体的沟通行为优先）；否则落到用户回合的保底行 chat/speak；再否则落到观赛默认 focus/focus。

别名表是历史包袱的消化器，也是两头必须一致的典型例子：

| 历史名 | 归一到 | 来源 |
| --- | --- | --- |
| cheer | celebrate | 早期庆祝组命名 |
| tense（表情） | nervous | 词表统一 |
| low / deflated（表情） | sad | 词表统一 |
| hold / settle / slump / nod | focus / idle / idle / agree | 早期合成名 |
| listening / confused（动作） | listen / idle | 映射表借用组名时的落点约定 |

事件行还携带投递调味（语气风格 + 保持窗）：goal 配 excited 语气和 2600ms，goal_cancelled 配低落语气和 2800ms，var_check 配 tense 语气 2200ms，shot_missed 配低落语气 2200ms。普通回合的语音能量与语速由情感向量算出：`0.35 + arousal×0.55` 与 `0.9 + arousal×0.15`，都有明确夹取范围。语气风格的合法词表是八个：natural、quiet、warm、excited、tense、low_disappointed、calm、soft——超出词表的风格值同样过不了客户端校验。

这张三层结构被多组测试从两个方向锁死：客户端的 `presentation_whitelist_test.dart` 与 `presentation_whitelist_contract_test.dart` 把代码里的常量、映射 JSON 和模型资产三方对齐；后端的 `presentation_table_test.go` 与 `presentation_vocabulary_test.go` 锁定 Go 镜像；仓库还有独立的 `scripts/check-presentation-map.mjs` 做离线校验。改任何一个名字，至少三处测试会同时红。

### 2.3 保持窗与回返模式：后端权威，客户端钳制

一个动作演多久、演完回到哪个身体，不允许前端即兴：

- **保持窗（holdMs）**。后端为每个计划给定保持时长，普通回合默认 1800ms，比赛事件行按信号性质给 2200–2800ms。客户端对它做 `clamp(0, 10000)`——后端忘了发就用 1800ms，发了离谱大值也最多保持 10s。保持窗内动作不会被待机层打断（谁来保证这一点，见 2.6 节的检查脚本）。
- **回返模式（returnMode）**。恰好四个合法值，每个对应一个真实落点：

| 回返模式 | 落点 | 语义 |
| --- | --- | --- |
| watching | focus / focus | 回到观赛专注体 |
| decay_to_focus | focus / focus | 表演自然消散后回到观赛 |
| decay_to_listening | listening / listen_01 | 回到等待用户下一句的听姿 |
| decay_to_idle | 待机三档轮换器 | 把身体交给 affect 驱动的待机 |

表演于是有了完整的生命周期：进场（映射表选型）、驻留（保持窗）、退场（回返模式落点），每一步都有明确的所有者。回返模式超出四值的计划同样会被客户端拒绝。

### 2.4 待机三档：安静的时候也要"像同一个人"

看球大部分时间是沉默的：用户在看，球球在陪。这段时间的身体状态决定了角色的"存在感质量"。我们的方案是**用关系的情感向量驱动待机**（`client/lib/services/idle_tier_picker.dart`）：

- **情感映射三档**。每份表演计划携带服务端情感状态机的 valence/arousal，客户端映射到三档待机：

| 档位 | 条件 | 待机动作 |
| --- | --- | --- |
| deflated 低落 | arousal ≤ 0.15 或 valence ≤ -0.35 | idle_01 |
| calm 平静 | 其余情况 | idle_02 |
| energetic 兴奋 | arousal ≥ 0.55 且 valence ≥ 0.15 | idle_03 |

球队落后时的沉默待机和领先时的沉默待机，身体语言不一样——这就是"陪看"和"挂机"的区别。阈值本身与后端情感状态机（`backend/internal/relationship/affect.go`）的基线数值耦合，两头一致，否则档位会系统性漂移。

- **30 秒重挑**。每 30 秒在当前档内重挑一次动作，避免一个循环动画播到底的僵尸感。
- **60 秒切档锁**。档位切换最短间隔 60 秒，防止情绪抖动让角色反复横跳。
- **0.1 迟滞**。情感向量必须越过目标档阈值再往外 0.1，才允许立即切档——一次微小的情绪波动不值得换身体，一次明确的情绪转变（比如绝杀）值得。
- **首次永不 energetic**。一次新的待机会话第一挑永远不落兴奋档。刚打开页面就手舞足蹈读起来像故障；兴奋是"挣来的"，得先有平静或低落的基线。

同样出于"安静不抢戏"的考虑，有几类特殊回合被刻规定了上限：终场告别的 wave 动作与告别语整场只用一次（`tests/evals/fulltime-farewell.spec.mjs` 锁定），挥完手就回待机；被打断的回合不补演，落 confused/listening 表达错愕就够了。待机的品质来自克制，不来自密度。

### 2.5 同信号幂等：只有关键事实修订允许刷新一次

比赛信号可能因为重连、重试而重复抵达。如果每次都重新演一遍进球庆祝，用户会在一次绝杀里看到三次绝杀。规则在 `backend/internal/relationship/director.go` 里：

- **同一个信号 ID 的决策只做一次**，重复请求直接返回已存决策——这是幂等底盘；
- **唯一的例外是关键事实修订**：信号是关键比赛事件、且携带了与原决策不同的事实修订号时，允许在原决策上刷新一次——同一个进球从"待确认"变成"确认有效"，话术和表情可以随修订后的事实更新一次。刷新数从 0 变 1，之后同样的信号再来，也只会拿到第一次刷新的结果；
- 刷新会留下 `critical_fact_refreshed` 的原因码，可审计；语音回合前后比分发生修订时，同文本会重生成，观察跟进也会被 in-band 刷新抑制，不会补一发过时的播报。

一条具体的时间线可以说明这条规则的价值：绝杀进球的信号第一次抵达，球球庆祝并播报"待确认"；两秒后确认事实带着新修订号再次抵达——这是合法的关键修订，话术与表情刷新一次，变成确定的庆祝；再往后无论这条信号重复到达多少次（重连回放也好、消息重试也好），球球都只会拿到第一次刷新后的那份决策，绝杀不会被演成连击。

### 2.6 idle-yield 检查脚本：动作不抢话

"身体同一时刻只有一个主人"这条规则，最容易在后来的改动中被悄悄改坏——比如有人调整说话收尾逻辑时，无意让待机定时器在表演保持窗内插进来一个 idle 动作。为此我们写了 `scripts/check-live2d-idle-yield.mjs`，它的工作方式很有代表性：**不重写逻辑，直接从 `client/assets/live2d/live2d.html` 里把真实实现提取出来跑**。

脚本用正则抽出页面里真实的 `scheduleIdle`、`armPresentationHold`、`setSpeaking` 函数与 `presentationHoldUntil`、`PRESENTATION_DEFAULT_HOLD_MS`、`motionAliases` 等声明，注入一个桩掉了 Date、setTimeout、document 的沙箱，然后驱动它们验证六件事：

1. 表演保持窗武装后，待机节拍让位，窗满了待机层才恢复；
2. 消息处理器里 `armPresentationHold` 必须先于 `setSpeaking` 调用——顺序反了，说话收尾的复位会抹掉刚武装的保持窗；
3. 停止说话的模式复位不会砍短存活的保持窗，`modeUntil` 不得低于保持窗截止；
4. 待机阶段的下发（包括 idle 档动作）不武装保持窗——待机层自己的动作不需要保护，避免自己锁自己；
5. 缺省 holdMs 回落到后端默认 1800ms；
6. 说话窗口永不缩短存活的保持窗。

这个脚本守住的其实是一条架构裁决：**表演保持的所有权在 Live2D 页面内的这一个函数组里**。谁改动了所有权边界——哪怕只是顺手调整了消息处理器的调用顺序——CI 立刻知道。

## 3. 如何验证

**phase-motions 端到端测试**（`tests/evals/phase-motions.spec.mjs`）。这是表演链路的主验收。测试用假麦克风注入受控音量（getUserMedia 假轨道 + AudioContext 逐帧重放说话音量，`__qReleaseSpeech` 控制放音时机），驱动完整真实链路：用户开口，球球进入听姿（listening/listen_01）；球球回话，身体切到说姿（chat/speak_01）。它验证的不是某个函数，而是"信号 → 映射 → 下发 → Live2D 页面实际换装"的整条通路。测试特意只在顶层帧安装假麦克风、不进 iframe，避免干扰 Live2D 页面自己的口型分析 AudioContext。

**idle-yield 脚本**（`scripts/check-live2d-idle-yield.mjs`）。如 2.6 节所述，从真实页面代码提取函数在沙箱中驱动，锁住待机让位规则。配套的 `scripts/check-presentation-map.mjs` 离线校验映射 JSON 本身。运行方式：

```bash
node scripts/check-live2d-idle-yield.mjs
node scripts/check-presentation-map.mjs
```

**契约与路由测试**。客户端 `presentation_whitelist_test.dart`、`presentation_whitelist_contract_test.dart`；后端 `presentation_vocabulary_test.go`、`presentation_table_test.go`（含空表情文件绑定禁令）。这些测试让"映射 JSON、客户端白名单、Go 镜像、模型资产"四方永远一致，任何一处单独漂移都会在 CI 里红掉。

**后端信号幂等**。director 的决策复用与关键修订一次刷新由关系层测试覆盖，保证"同一信号不重演"不依赖前端自觉。

这套表演验收不是孤立跑的：phase-motions 属于 tests/evals 的 Playwright 验收族，与 fulltime-farewell（告别一次性）、client-voice-runtime（语音回合的表演交接）等 spec 共享同一套测试支撑（假麦克风、测试账号、本地服务编排）；两个检查脚本与前后端单测一起挂在仓库的常规验证路径上。表演行为的任何改动，都要在这四层里同时交代得过去，才算改完了。

四层验证各守一段，合起来是这条链路的完整回归面：

| 验证层 | 文件 | 守住什么 |
| --- | --- | --- |
| 端到端换装 | `tests/evals/phase-motions.spec.mjs` | 假麦克风说话出听姿、球球回话出说姿 |
| 页面所有权 | `scripts/check-live2d-idle-yield.mjs` | 待机在保持窗内让位、调用顺序、默认保持窗 |
| 映射一致性 | `check-presentation-map.mjs` + 双端四份契约测试 | JSON、客户端白名单、Go 镜像、模型资产四方一致 |
| 信号幂等 | 关系层 Go 测试 | 同信号只演一次、关键修订只刷新一次 |

## 4. 边界与不做

- **不做口型逐字同步，只做 energy 级风格**。我们不为台词逐字排口型时间轴——那是把假做真的无底洞。实际的口型由真实播放的音频驱动：Live2D 页面通过 wLipSync（WASM 版 MFCC 分析，随资产一起分发）从播放中的音频实时提取口型参数，喂给模型的嘴部骨骼参数（张嘴、嘴形、圆唇、拉伸）；分析器不可用时退回音频能量驱动。而"演技"层面只有三个能量级方向词——语气风格（style）、能量（energy）、语速（speed），由 `backend/cmd/server/tts_style.go` 转成中文方向词拼进固定人设的长指令交给 TTS。风格有方向，没有逐帧编排。
- **不做情感诊断**。映射表输入的是信号类型与情感向量的区间，不是"角色此刻的心情"。表情是呈现层的着色，角色不因一套动作就被认定拥有了真实情绪，我们也从不让动作暗示用户欠它什么。
- **不让前端即兴**。前端没有任何一条路径可以凭文本内容自选高强度表情；白名单外的一切落待机。这条限制曾经让一些"更灵活"的想法落不了地，但它是"像在反应"能长期成立的前提——反应的可信来自每次都对，而不是偶尔惊艳。
- **不做无声抢戏**。TTS 失败时字幕兜底，口型与动作不装作"正在说话"；被打断的回合落到映射表 delivery 段规定的 confused/listening，而不是把没说完的话演完。
- **不做一次性时刻的复用**。开场 hello、终场 wave 这类仪式性动作每场只出现一次，反复使用会让仪式贬值成背景动画。

舞台的边界画在哪里，和舞台有多生动是同一件事。回头看，这个舞台由六个机制共同撑起：

- 一份 `presentation-map.json` 作为唯一事实源，13 表情、17 动作、8 行为、5 事件、6 阶段各有归属；
- 两端互锁的白名单与别名归一，非法名字落待机；
- 后端权威的保持窗（默认 1.8s、客户端钳到 10s 上限）与四值回返模式；
- 情感向量驱动的三档待机，30s 重挑、60s 切档锁、0.1 迟滞、首挑永不兴奋；
- 同信号幂等，只有关键事实修订允许刷新一次；
- 一个从真实页面提取实现来跑的检查脚本，守着"动作不抢话"的所有权裁决。

它们共同保证：球球的每一个表情都有出处，每一段表演都会收场，而它安静陪你看球的样子，前后是同一个人。

---

版本 2.0（2026-09-20）
