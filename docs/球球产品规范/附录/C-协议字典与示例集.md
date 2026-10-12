---
篇号: "C"
标题: 附录 C · 协议字典与示例集
性质: 附录（规范件——/ws/match 上行 20 类与下行 19 类的 wire 字典唯一登记面；wire JSON 示例全集落本篇，18 篇只立合同与枚举，两处互链不重复）
状态: active
版本: v3.0
基线: a7e5100（2026-06-07）
文体: reference（查表体：消息字典表 + wire 示例集 + 非法值警示；论证入篇末设计说明）
上游: 后端现码 WS 与数据字典逐项对账；PRD-球球-v3.0.md 第八章 8.4（统计与入口）；迁移源 v2 档案 附录D/附录F（示例教训与延迟预算吸收，原文件退役）
评测挂钩: 协议枚举对账——18 篇 §3.2/§3.3 行数（20/19）与三套回执枚举是验收断言本身；runtime-e2e.mjs 真实 WS 话轮走查本篇 §5 示例时序
裁决点: ⚖️B12（合同扩至现网全集——本篇即 WS 全集字典底稿；talkativeness_ack 消费口径注记见 §8）
---

# 附录 C 协议字典与示例集

> 本篇基线：2026-06-07 仓库 a7e5100。

**本篇定位**：/ws/match 通道 39 类消息（上行 20 / 下行 19）的逐条 wire 字典、三套回执枚举全集、10 个高频消息的 wire 示例、已知非法值警示清单。合同语义（鉴权三态、连接保护、送达合同、失败语言）归 18 篇，本篇不重复；HTTP 端点全集归 18 篇 §2，本篇只留指针（§7）。**每个 wire 示例标注字段值验证出处**——v2 附录D 曾因示例值越界（`attentive`/`lean_in`/`energetic`/`chat`）被现网拒绝，本篇示例全部取自代码实发值（教训注记见 §6.3）。

**导读**：§1 字典总纲｜§2 上行字典 20 类｜§3 下行字典 19 类｜§4 回执枚举全集｜§5 wire 示例集（10 例）｜§6 已知非法值警示｜§7 HTTP 端点速查指针｜§8 设计说明。

**事实依据**：消息全集与字段面逐条对照 `backend/cmd/server/watchconnection.go:991-1300`（读循环 20-case）、各生产发送点（`transcription.go`/`turn_relay.go`/`response_delivery.go`/`delivery.go`/`ops_stream.go`）现码核对（a7e5100）；示例值经代码验证（出处逐例标注）。

---

## 1. 字典总纲

| # | 约定 | 内容 |
| --- | --- | --- |
| 1 | 通道 | `/ws/match/{matchId}`——用户陪看流（本篇主体）；`/ws/ops` 运营观测流（§5 例 10）；两者消息面互不相通 |
| 2 | 编码 | 全协议 JSON（UTF-8），字段 camelCase（`deliveryKey`/`utteranceId`/`traceId`），枚举值小写蛇形（`text_fallback`/`turn_planned`）；`voice_audio` 为「文本元数据帧 + 二进制音频帧」成对发送 |
| 3 | 无统一信封 | 每类消息自带语义（`type` + 自需字段），无共享包装层；唯一生成规则是状态回执按 `DeliveryStatus.Kind+"_status"` 动态拼名（`voice_status`、`first_meeting_status` 由此产生） |
| 4 | 行数即锚点 | 上行 20 / 下行 19 是对账锚点：读循环 case 数或生产发送点数变化即破坏性变更，须同时改 18 篇与本篇 |
| 5 | 双反幻影 | `welcome` 不是 /ws/match 消息（首条下行是 `match_snapshot`）；`reconnecting` 不是 wire 消息（客户端本地合成）。两者不进字典，见 §6.1 |
| 6 | 关联键 | `utteranceId`（一次语音输入全生命周期）、`signalId`（话轮级，缺省服务端生成 `turn_<userId>_<nanotime>`）、`deliveryKey`（投递去重，五种键格式族）、`traceId`（回合观测，格式 `trace_<unixnano>_<seq>`） |
| 7 | 机器可读形态（规划中（未实现，P2）；2026-06-09 对标增补） | 本篇人读字典为唯一源，未来生成 **AsyncAPI 形态机器可读规范**（随埋点 ⚖️B22 裁决同批选型；对称先例＝31 篇 §7.1 schema 注册表），CI 校验字典与 schema 同源、禁止两处各自演化；落地前「行数即锚点」人工对账（本篇约定 4）＋代码测试锚（console_golden_test.go、runtime-e2e.mjs）仍为唯一对账纪律 |

---

## 2. 上行字典：客户端 → 服务端 20 类

产生时机与服务端处理为合同面；字段名经 `watchconnection.go:991-1300` 读循环逐 case 核对。

| # | type | 关键字段 | 产生时机 |
| --- | --- | --- | --- |
| 1 | `ping` | — | 客户端 25s 周期心跳 |
| 2 | `identify` | userId | 连接建立后声明身份（与凭据不符回 `auth_error` 断开） |
| 3 | `set_talkativeness` | talkativeness（quiet/normal/active） | 用户改话痨档；即时持久化，回 `talkativeness_ack`；未知值回落 normal |
| 4 | `set_character` | field / value | 用户改人格互动规范（三入口一状态之 WS 入口）；落 `character_setting` 账本行，回 `character_ack` |
| 5 | `user_activity` | state（speaking/idle，服务端只判 speaking） | 用户说话状态变化；speaking 时取消查询、打断主动发言排程 |
| 6 | `session_opened` | userId | 进入比赛页/重连成功；按原 deliveryKey 回放未送达回复、恢复待定观察与话题回合 |
| 7 | `session_closed` | userId / reason | 离开比赛页；服务端释放会话资源并回执 |
| 8 | `first_meeting` | userId / nickname / favoriteTeam | 初见未完成时提交资料；安排确定性四变体问候（30s TTL、幂等静音门）；昵称截 20 rune、主队截 24 rune |
| 9 | `interrupt` | — | 用户主动打断；服务端取消查询+停排程，回 `interrupt`（expression=listening） |
| 10 | `user_speech` | text、audio（可双带）、talkativeness、signalId、timezone（客户端自动附加） | 提交一个完整话轮（文字或整段音频）；也是断线补发载体；记延迟锚点 speech_received |
| 11 | `asr_start` | utteranceId / signalId / timezone / 识别提示 | 一次流式识别开始（用户动作开启）；每连接在途识别会话上限 8，超限 `transcript_error` |
| 12 | `asr_chunk` | utteranceId / sequence / audio（base64 PCM） | 流式音频分片；按 sequence 重组，乱序/超长（30s 整段上限）拒绝；合法分片并行旁送气氛 sidecar（默认关、失败静默） |
| 13 | `asr_finish` | utteranceId | 说完信号；结束识别出终稿，记延迟锚点 asr_finish |
| 14 | `asr_cancel` | utteranceId | 用户中途放弃；片段即刻停用，取消后不进识别 |
| 15 | `voice_playback` | traceId / state（六值枚举 §4.1） | 回合语音播放状态变化；推进投递七态+调度器 PlaybackChanged |
| 16 | `reply_displayed` | traceId | 文字回复已上屏；投递态推进 text_delivered |
| 17 | `playback_result` | deliveryKey / state（四终态枚举 §4.2）/ reason? | 播放终态实报；迟到回执只记 `client_late`，不回改既有终态 |
| 18 | `duplex_event` | event（三事件 §4.5）/ streak / transcript | 播放期抢话遥测（判定在客户端）；只落结构化日志+客户端健康账本（正文不落，只记 rune 数） |
| 19 | `lipsync_event` | reason（三值 §4.5） | 嘴型退化遥测（客户端限频后的状态沿）；只落日志+健康账本，不参与判定 |
| 20 | `turn_query` | utteranceId / text | 说完判定 model 插槽提前问（静默累计 600ms 起）；独立协程转发 turn sidecar（预算 500ms、在途上限 4、饱和回 null）；文本不入日志 |

---

## 3. 下行字典：服务端 → 客户端 19 类

全集口径=生产发送点清点（测试构造的字面量不计入）；`voice_status`/`first_meeting_status` 由 `DeliveryStatus.Kind+"_status"` 动态拼名产生。

| # | type | 关键字段 | 产生时机 |
| --- | --- | --- | --- |
| 1 | `match_snapshot` | data=公开快照（`clientSnapshot` 剥除完整性调试字段） | 连接建立首条、时钟轮播后、每条事件后；界面整体校准的唯一权威 |
| 2 | `match_clock` | version / period / elapsed / running / anchorAt | 时钟更新；**每条时钟推送后紧跟一条新快照** |
| 3 | `match_event` | data=MatchEvent、snapshot、deliveryKey（`事件ID:factRevision:factStatus`） | 一条公开事实成立；客户端按 deliveryKey 去重、按修订号丢弃旧版 |
| 4 | `match_fact_retracted` | eventId / factId | 一条事实被撤销；撤下由该事实生成的回复与待播音频 |
| 5 | `event`（`event=qiuqiu_reply`） | data：text / traceId / source / deliveryKey / eventId? / presentation? / reason? | 对话回复就绪；source=`recovered_delivery`=重连回放、`first_meeting`=初见（TTL 30s）、`conversation`=常规聊天回复 |
| 6 | `presentation` | 表演计划（PresentationPlan）+ eventId + deliveryKey + source + citation | 仅动作与表情计划（无正文）；驱动 Live2D 表演 |
| 7 | `transcript_partial` | utteranceId / revision / text / provider | 流式识别中间结果；旧 revision 不覆盖新 |
| 8 | `transcript_final` | utteranceId / revision / text / provider | 识别终稿；字幕定稿并交对话层 |
| 9 | `transcript_error` | utteranceId / revision / reason / **recoverable** | 识别出错；recoverable=true 不终止会话（本句可重说），false 终止转写链 |
| 10 | `voice_status` | state（三态 §4.3）/ reason? / traceId? | 语音链路状态变化：text_fallback=切文字继续；tts_fallback=字幕兜底；failed=本句失败说明 |
| 11 | `first_meeting_status` | state（delivered / skipped）/ reason? / traceId? | 初见问候投递终态；skipped 时 reason=`first_meeting_already_delivered` |
| 12 | `talkativeness_ack` | tier / persisted | `set_talkativeness` 处理完；服务端回执（客户端当前未消费，⚖️B12 注记见 18 篇 §7.2） |
| 13 | `character_ack` | field / value / persisted | `set_character` 处理完；与 HTTP PATCH /api/me/character 三入口一状态 |
| 14 | `turn_result` | utteranceId / isComplete（true/false/**null**）/ probability? | turn_query 的转发结论；null=未配置/超时/熔断/4xx——客户端插槽下探静默档 |
| 15 | `voice_audio` | 文本元数据帧（mime / traceId / eventId / deliveryKey / source / byteLength）+ 二进制音频帧成对 | 回合语音投递（整段）；backchannel 变体无 traceId/eventId、带「（小声）」text 微标注；按 deliveryKey 去重 |
| 16 | `interrupt`（下行回执） | expression=listening | `interrupt` 上行处理完；球球转聆听表演的确认 |
| 17 | `pong` | — | `ping` 应答，心跳保活 |
| 18 | `auth_error` | reason（身份不符 / identity required / session expired or revoked） | 凭据不符/过期/吊销（7 处发送点）；客户端走通用重连阶梯 |
| 19 | `session_closed`（下行回执） | reason | `session_closed` 上行处理完；本场结束确认 |

---

## 4. 回执枚举全集（三套并存 + 配套词典）

### 4.1 voice_playback 六值（上行）

| 值 | 语义 | 投递七态推进 |
| --- | --- | --- |
| `started` | 播放开始 | audio_started |
| `ended` | 播放自然结束（completed 为其别名） | completed |
| `completed` | ended 别名 | completed |
| `interrupted` | 播放被打断 | interrupted |
| `skipped` | 播放被跳过 | skipped |
| `blocked` | 系统拦截播放——**合法别名**，双端映射到 skipped（客户端 `playbackResultState` + 服务端 `deliveryStateForPlayback` 两处） | skipped |
| 其他未知值 | — | failed |

### 4.2 playback_result 四终态（上行）

| 值 | 语义 |
| --- | --- |
| `completed` | 播放完整结束 |
| `interrupted` | 播放被打断 |
| `skipped` | 播放被跳过（blocked 经映射进入此值） |
| `failed` | 播放失败 |

> **无 `started`**：开始状态走 voice_playback；playback_result 只报终态。迟到回执（投递已有终态）只记 `client_late` 追加账本，不回改终态。

### 4.3 voice_status 三态（下行）

| 值 | 语义 |
| --- | --- |
| `failed` | 语音链路本句失败（附 reason） |
| `text_fallback` | 识别失败，切文字继续 |
| `tts_fallback` | 合成失败，字幕兜底 |

> **`truncated` 不是现役枚举值**：句粒度拆除（e35c3fe）后服务端不再产生；客户端残留分支为死代码，集成方不得等待该值（§6.1）。

### 4.4 配套词典

| 词典 | 全集 | 载体 |
| --- | --- | --- |
| first_meeting_status 两态（下行） | delivered / skipped | 初见问候投递回执 |
| turn_result.isComplete 三值（下行） | true / false / **null** | null=插槽下探静默档 |
| duplex_event 三事件（上行） | self_interrupt_suspected / duplex_degraded / duplex_recovered | 客户端健康遥测 |
| lipsync_event 三 reason（上行） | lipsync_vendor_unavailable / lipsync_decode_failed / lipsync_jitter_fallback | 客户端健康遥测（健康事件 6 种全集=duplex 三事件+lipsync 三 reason） |
| 投递七态（账本） | planned → text_delivered / audio_started → completed / interrupted / skipped / failed | delivery_ledger，两条播放回执上行共同推进 |
| 话痨三档 | quiet / normal / active（默认 normal；未知值回落 normal） | set_talkativeness / talkativeness_ack 的 tier 值 |
| FactStatus 五态 | provisional / confirmed / revoked / conflict / reconciled | deliveryKey 第三段与 match_event 携带的 factStatus |

### 4.5 边界：不是 wire 消息的「消息」

| 名目 | 真实身份 | 处置 |
| --- | --- | --- |
| `reconnecting` | 客户端 websocket_service.dart 本地合成进消息流供 UI 显示（「正在回到比赛现场…」） | 服务端零收发；协议清单不收录 |
| 客户端连接状态五态 | connecting / connected / reconnecting / disconnected / failed（SocketStatus，纯本地） | 不上线 |
| 会话相位 11 态 | idle/welcoming/listening/userSpeaking/understanding/speaking/permissionDenied/offline/failed/reconnecting/recovered（MatchSessionPhase，本地） | 由 wire 消息驱动迁移，相位本身不上线 |

---

## 5. wire 示例集（10 例，字段值经代码验证）

> 示例口径：字段名与字段值全部对照现码（a7e5100）；为可读而省略的字段以注释标注，不作虚构。每例尾注「验证」行给代码出处。

### 例 1 `match_snapshot`——连接首条下行

```json
{
  "type": "match_snapshot",
  "data": {
    "matchId": "m-1024",
    "clock": { "period": "second_half", "elapsed": 3120, "running": true },
    "score": { "home": 1, "away": 0 },
    "events": []
  }
}
```

验证：`watchconnection.go:312/616/640`（发送点）；data=公开快照 `clientSnapshot` 剥除完整性调试字段（`main.go:1024`）——示例内部结构为公开视图摘要字段，完整形状以 `/api/matches/{id}/state` 公开视图同源。真实首条下行是它，不是 welcome（§6.1）。

### 例 2 `match_clock`——时钟推送（后跟新快照）

```json
{
  "type": "match_clock",
  "version": 42,
  "period": "second_half",
  "elapsed": 3180,
  "running": true,
  "anchorAt": "2026-06-07T15:02:11Z"
}
```

验证：字段集 `watchconnection.go:317/612`；period 六态枚举 `pre_match/first_half/halftime/second_half/extra_time/fulltime`（迁移 021 CHECK）；elapsed ≤21600（同 CHECK）。合同：每条时钟推送后紧跟一条新快照（例 1 复用）。

### 例 3 `match_event`——VAR 核查事件（var_check）

```json
{
  "type": "match_event",
  "data": {
    "id": "evt-1024-83",
    "matchId": "m-1024",
    "source": "espn",
    "period": "second_half",
    "clock": "78:12",
    "eventType": "var_check",
    "intensity": 3,
    "confirmed": true,
    "visibility": "public",
    "status": "active",
    "factId": "fact-5521",
    "factRevision": 1,
    "score": { "home": 1, "away": 0 },
    "description": "VAR 正在核查一次禁区内疑似手球",
    "createdAt": "2026-06-07T15:04:31Z",
    "updatedAt": "2026-06-07T15:04:31Z"
  },
  "snapshot": { "…": "公开快照，同例 1 data 形状" },
  "deliveryKey": "evt-1024-83:1:confirmed"
}
```

验证：data 字段名=MatchEvent struct JSON tag 全集（`matchstate/store.go:158` 起）；`var_check` ∈ 自动播报事件类型域 21 值（迁移 009）；source=`espn` ∈ 四值域（`manager.go:16-20`）；deliveryKey 三段式=`事件ID:factRevision:factStatus`（`store.go:198`），三段值均合法枚举（同形样例 `evt-1024-83:1:confirmed` 见本篇例 3 与例 9；键格式族合同见 18 篇 §1）。

### 例 4 `event`（qiuqiu_reply）——常规聊天回复

```json
{
  "type": "event",
  "event": "qiuqiu_reply",
  "data": {
    "text": "这球进了！1 比 0，账本上已经确认了。",
    "traceId": "trace_1728306271000000000_1",
    "source": "conversation",
    "deliveryKey": "trace_1728306271000000000_1",
    "presentation": { "…": "PresentationPlan，独立示例见例 6" },
    "reason": { "codes": ["policy_user_setting:talkativeness"], "citation": "" }
  }
}
```

验证：外层 `{"type":"event","event":"qiuqiu_reply","data":{…}}`（`response_delivery.go:44-48`）；data 字段面 `qiuqiuReplyData`（`main.go:141-154`，text/traceId/source 必填，eventId/deliveryKey/presentation 条件出现，reason 可选 `{"codes":[],"citation":""}`，`response_delivery.go:20-28`）。**source 现网值为 `conversation`——不是 `chat`**（`watchconnection.go:850`）；traceId 真实格式 `trace_<unixnano>_<seq>`（`agent.go:1946`）；回复 deliveryKey=ForTrace(traceId)（`deliverykey.go:37-39`）。

### 例 5 `presentation`——打断反应表演计划（无正文）

```json
{
  "type": "presentation",
  "expression": "confused",
  "motion": "listening",
  "voiceStyle": "natural",
  "voiceEnergy": 0.4,
  "voiceSpeed": 1.0,
  "holdMs": 1800,
  "returnMode": "decay_to_focus",
  "affect": {
    "valence": -0.1, "arousal": 0.35, "tension": 0.4,
    "confidence": 0.6, "engagement": 0.5,
    "updatedAt": "2026-06-07T15:05:02Z"
  },
  "source": "delivery_interrupted",
  "deliveryKey": "delivery-interrupted:trace_1728306271000000000_1"
}
```

验证：打断反应行代码实发 `expression:"confused", motion:"listening", voiceStyle:"natural"` + `holdMs:1800` + `returnMode:"decay_to_focus"`（`relationship/presentation_table.go:277-287` InterruptedDeliveryPresentation）；三值均在白名单（expression 13 值 / motion 23 接受值 / voiceStyle 8 值，`presentation_vocabulary.go` ≡ `presentation_state.dart`）；returnMode 四值之一（`presentation_state.dart:176-181`）；affect 六字段必下发（`types.go:395-404`，无 omitempty）；source=`delivery_interrupted` 与 `delivery-interrupted:` 前缀逐字命中（`delivery.go:256,266`）。**v2 附录D 同位置示例的 `attentive`/`lean_in`/`energetic` 均为白名单外非法值**（§6.2）。

### 例 6 回复内嵌 PresentationPlan（qiuqiu_reply 的 presentation 字段）

```json
{
  "expression": "excited",
  "motion": "cheer",
  "voiceStyle": "excited",
  "voiceEnergy": 0.9,
  "voiceSpeed": 1.05,
  "holdMs": 1800,
  "returnMode": "watching",
  "affect": { "…": "同例 5 六字段" }
}
```

验证：进球庆祝行代码实发 `Expression:"excited", Motion:"celebrate", VoiceStyle:"excited", VoiceEnergy:0.9, VoiceSpeed:1.05, HoldMS:1800, ReturnMode:"watching"`（`presentation_table.go:298`）；`cheer` 为 legacy 合成名、白名单接受（别名归一后放行，`presentation_vocabulary.go`）；`celebrate` 同为 12 动作组之一。三向锁保证四镜像（Go/Dart/映射表/模型资产）一致，白名单外值客户端解析返回 null——整个表演计划被丢弃、正文照播无表演（§6.2 后果列）。

### 例 7 `turn_result`——说完判定结论（null 分支与命中分支）

```json
{"type": "turn_result", "utteranceId": "utt-20261007-001", "isComplete": null}
```

```json
{"type": "turn_result", "utteranceId": "utt-20261007-001", "isComplete": false, "probability": 0.0031}
```

验证：payload 构造 `{"type":"turn_result","utteranceId":…,"isComplete":null}`，resolved 时才填 isComplete 与 probability（`cmd/server/turn_relay.go:176-183` 逐字）；null 语义=未配置/超时/熔断/4xx——客户端插槽下探静默档，服务端零特殊分支。zh 阈值 ≈0.0066 量级（`turn-sidecar/app.py`），示例 probability 0.0031 取该量级合法值。

### 例 8 `voice_audio`——整段语音投递（双帧成对）

```json
{
  "type": "voice_audio",
  "mime": "audio/wav",
  "traceId": "trace_1728306271000000000_1",
  "eventId": "",
  "deliveryKey": "trace_1728306271000000000_1",
  "source": "conversation",
  "byteLength": 88204
}
```
（随后同一连接发送二进制音频帧）

验证：元数据帧字段面=`websocketResponseSink.DeliverAudio` 逐字（`response_delivery.go:50-60`：mime/traceId/byteLength/eventId/deliveryKey/source）；文本帧+二进制帧成对（voice_audio 行）。backchannel 变体差异：无 traceId/eventId、deliveryKey=`backchannel-<事件ID>`、source=`backchannel`、text 微标注「（小声）…」（`backchannel_audio.go:49-58`）。客户端按 deliveryKey 去重（音频 FIFO 配对，469df17）。

### 例 9 `playback_result`——播放终态实报（上行）

```json
{"type": "playback_result", "deliveryKey": "trace_1728306271000000000_1", "state": "completed"}
```

```json
{"type": "playback_result", "deliveryKey": "evt-1024-83:1:confirmed", "state": "skipped", "reason": "user muted"}
```

验证：读循环 case 逐字（`watchconnection.go:1250-1262`：deliveryKey/state/reason 三字段，`validPlaybackResultState` 校验四终态之一，非法值静默丢弃）；服务端按 deliveryKey 定位投递记录推进七态，迟到回执只记 `client_late`。**客户端本地 `blocked` 不以此形态上报**——本地播放状态经 `playbackResultState` 映射后 `blocked→skipped`、`ended|completed→completed`、`started` 不报（`match_session_controller.dart:1611-1617`）。

### 例 10 `/ws/ops`——运营观测流（welcome 与事件）

```json
{"type": "welcome", "stream": "ops"}
```

```json
{
  "type": "ops_event",
  "kind": "turn_planned",
  "matchId": "m-1024",
  "userId": "usr_9f8e7d6c",
  "traceId": "trace_1728306271000000000_1",
  "latencyMs": 4120,
  "at": "2026-06-07T15:05:06Z"
}
```

验证：首条下行 `{"type":"welcome","stream":"ops"}` 逐字（`ops_stream.go:208`）——**全库唯一现役 welcome**；事件 wire 形状=`OpsEventWire` 结构体逐字段（`ops_stream.go:26-36`：type 恒 `ops_event`、kind/matchId/userId/traceId?/deliveryState?/playbackState?/latencyMs?/at）；`latencyMs` 仅 `turn_planned` kind 从 trace 载荷提取；正文字段（phrase/inputText/outputText/decision/presentation/trace 载荷）在序列化前被裁剪——结构体层面不存在，非「约定不填」。kind 9 类全集见 18 篇 §3.6。用户会话误升级此通道即被拒（ClosePolicyViolation）。

### 5.1 一轮语音话轮的示例时序（例 3-例 9 的串联走查）

| 步 | 方向 | 消息（对应示例） |
| --- | --- | --- |
| 1 | C→S | `asr_start`（utteranceId）→ 服务端开识别会话 |
| 2 | C→S / S→C | `asr_chunk` ×N ⇄ `transcript_partial` ×N |
| 3 | C→S / S→C | `turn_query` → `turn_result`（例 7；null 即下探静默档） |
| 4 | C→S / S→C | `asr_finish` → `transcript_final` |
| 5 | S→C | `event`（qiuqiu_reply，例 4）+ `presentation`（例 5/6）+ `voice_audio`（例 8，双帧） |
| 6 | C→S | `voice_playback{started}` → 投递态 audio_started |
| 7 | C→S | `duplex_event`（如有抢话）或 `interrupt` |
| 8 | C→S | `voice_playback{ended}` + `playback_result{completed}`（例 9）+ `reply_displayed` |

异常支线（任一步切入，全部落回文字）：`asr_cancel` 中途放弃｜`transcript_error`（recoverable 链）→ 整段重试 → `voice_status{failed}`｜`voice_status{tts_fallback}` 字幕兜底｜`match_fact_retracted` 撤回待播内容｜断线 → 本地合成 reconnecting（不上线）→ 重连 → `session_opened` 回放（source=`recovered_delivery`）→ 补发 ≤4 条 FIFO。

---

## 6. 已知非法值警示

### 6.1 幻影消息与死值（集成方不得等待）

| 名目 | 现实 | 后果与处置 |
| --- | --- | --- |
| `welcome`（/ws/match 语境） | /ws/match **从不发送**；真实首条下行是 `match_snapshot`。welcome 仅存于在用的 /ws/ops（`ops_stream.go:208`，例 10，全库唯一现役）——旧「main.go 死桩」说法已销项（与附录 B §8.1、PRD 修订记录修-8 同批口径） | 等待 welcome 握手确认的客户端永久超时；v2 附录F 幻影 F9 已清除 |
| `reconnecting`（作为通道消息） | 客户端本地合成事件（`websocket_service.dart:181`），服务端零收发 | 不得列入协议清单或服务端测试断言 |
| `voice_status` 的 `truncated` | 句粒度拆除后服务端不再产生；客户端残留分支为死代码 | 不入枚举、不入断言；分支清理列工程侧代码卫生 |
| `welcome` 旧字段形态 | v2 附录D 示例 `{"type":"welcome","message":"connected"}` 来自未注册死桩（ws/handler.go:216） | `/ws/ops` 现役形态是 `{"type":"welcome","stream":"ops"}`（例 10） |

### 6.2 白名单外示例值（v2 附录D §5 三连违约的教训注记）

| 非法值 | 出现位置 | 现网后果（代码实锤） | 合法替代（本篇示例已用） |
| --- | --- | --- | --- |
| `attentive`（expression） | v2 附录D presentation 示例 | `normalizeExpression` 返回 null → parse null → **整条 presentation 消息被客户端丢弃**（`match_screen.dart:554`） | 13 白名单值任一（示例用 `confused`/`excited`） |
| `lean_in`（motion） | 同上 | 同上（双端同拒） | 23 接受值任一（示例用 `listening`/`cheer`） |
| `energetic`（voiceStyle） | v2 附录D qiuqiu_reply 示例 | 不在 8 值白名单 → parse 返回 null → **表演计划整体丢弃，正文照播无表情动作**（`presentation_state.dart:246-250` 四查之一） | 8 值任一（示例用 `natural`/`excited`） |
| `"chat"`（source） | v2 附录D qiuqiu_reply 示例 | 非现网值（全仓 grep 零命中）；客户端不校验故不拒，但示例非真实 | `conversation`（例 4） |

### 6.3 示例编写纪律（防回潮）

1. **示例值必须在白名单**：任何 wire 示例的枚举字段值，落笔前对照本篇 §4 与 10 篇白名单三向锁核对；「示意值」不是豁免理由——文档示例会被集成者逐字抄进客户端。
2. **新示例入本篇**：18 篇只立合同与枚举（兼容约定 6）；新增/修订 wire 示例一律落本篇并标注验证出处，示例与本篇字典冲突时以代码发送点为准并回改两处。
3. **反幻影三查**：交付前 grep 三项——`welcome`（只允许 /ws/ops 语境）、`reconnecting` 作为通道消息、`truncated` 作为现役枚举、`attentive`/`lean_in`/`energetic`/`chat` 作为示例值；命中即改（18 篇 §7.4 同款执法）。

---

## 7. HTTP 端点速查指针（全集在 18 篇，不重复）

| 面 | 统计 | 入口 |
| --- | --- | --- |
| 控制面 | **51 端点**（ServeMux 三表：console 19 / match 运营面 28 / knowledge 4）；幂等写+审计；405 带 Allow、尾部斜杠不容忍 | 18 篇 §2.1-2.3 逐条表（scope/幂等性列） |
| 用户面族 | sessions（anonymous/login/refresh/revoke）+ me（privacy/export/data/character/portrait/threads/moments/journal）+ catalog + health——表外族不计入 51 | 18 篇 §2.4（21 行） |
| 只读旁路 | /ws/ops、/mcp（匿名一律 401，4 只读工具）、/grafana（同源反代） | 18 篇 §2.5 |
| 鉴权三态 | 匿名会话 / C 端登录（原地绑凭证）/ 运营 JWT；WS 凭证走子协议 `qiuqiu-auth.` | 18 篇 §1.2 |
| 错误码 | 201/202/400/401/403/404/405/409/410/429/501 全约定 | 18 篇 §2.6 |

---

## 8. 设计说明（写法依据与迁移差异，非需求条款）

1. **迁移映射**：本篇承接 v2 档案《附录D-比赛事实状态机、数据字典与消息示例》《附录E-关系、偏好与用户边界数据字典》《附录F-实时语音协议、状态码与延迟预算》三篇的协议与示例职能。附录D §5 的 JSON 示例按体检结论逐处修复（非法值换合法值、缺失字段补全、字段值标注验证出处）；附录F 的 WS 清单按对账精确缺单并入本篇 §2/§3（上行缺 9/20、下行缺 6/19、三套回执枚举零记载、welcome 幻影）；附录F 的延迟预算表归 18 篇 §4、降级矩阵归 06/20 篇；附录E 的数据字典归 17 篇。
2. **示例从「叙述化」回到「wire 化」**：对账证实 v2 五篇正文没有任何 JSON 示例（v2.0 已全部叙述化），集成者只能靠读代码取枚举。v3 反向修正：合同归 18 篇、示例归本篇，两处互链防重复漂移——示例是合同的执行样张，不是装饰。
3. **「字段值经代码验证」的含义**：字段名对照结构体 JSON tag 或 map 字面量（发送点/读循环逐 case）；字段值对照代码实发行（如打断反应行、进球庆祝行）或合法枚举域；省略的字段以「…」注释标注而不虚构。三元组（例 1 快照内部结构、例 4 reason 属性值）属摘要级省略，已逐例注明。
4. **⚖️B12 注记**：本篇即 WS 全集（20/19）字典底稿（合同扩至现网全集的落点之一）；`talkativeness_ack` 下发为真、客户端当前未消费——回执保留在合同中，消费口径挂 ⚖️B12（登记见 37 篇）。
5. **与 18 篇的对账纪律**：本篇 §2/§3 行数（20/19）与 18 篇 §3.2/§3.3 同源同数；协议消息的新增/删除必须同时落两篇并跑协议枚举对账断言；本篇 §5 示例被 runtime-e2e 的真实话轮走查间接验证（连接→快照时钟→user_speech→回合投递→回执收尾）。

---

## 修订记录

| 版本 | 日期 | 依据 | 变更摘要 |
| --- | --- | --- | --- |
| v3.0.1 | 2026-06-09 | 复核修复（元数据缺项） | YAML 补「篇号」字段（取值附录字母，00 篇 §6.3）；补设本修订记录节 |
| v3.0.2 | 2026-06-09 | 对标补齐（benchmark-r4 B4；gap-system.md） | §1 字典总纲新增约定 7「机器可读形态」：以本篇为唯一源未来生成 AsyncAPI 形态机器可读规范（随埋点 ⚖️B22 裁决同批选型，对称先例 31 篇 §7.1），标规划中（未实现，P2）；落地前行数对账＋代码测试锚仍为唯一纪律（与 18 篇 §5 约定 8 同批条款） |
| v3.0.3 | 2026-06-09 | 终审收口（终审报告 P2：新增节题标注统一） | §1 约定 7 行「2026-06-09 对标补齐」改「2026-06-09 对标增补」（全库新增节标注统一；与 18 篇约定 8 同批） |
| v3.0.4 | 2026-06-09 | 主编通读修订（E1；读者报告 C D14/D15） | §6.1 welcome 行死桩表述销项统一：改「welcome 仅存于在用的 /ws/ops（ops_stream.go:208），/ws/match 不发送」，删「死桩删除列工程侧待办」失准句（与附录 B §8.1、PRD 修-8 对齐）；例 3「55 章 §2.5 同形样例」死引改本篇例 3/例 9 与 18 篇 §1 现行指针 |
