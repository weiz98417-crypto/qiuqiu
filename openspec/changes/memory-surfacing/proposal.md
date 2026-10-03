# 记忆可感面:已建产能翻译成用户可见(B1)+未完话题账本统一(A1)

## Why

agent-depth 轮建好的四块记忆产能——Portrait、Open Thread、主动回合理由码、Interaction Ledger——在客户端是零表面:主动回合与普通回复同管道同形态、无来源标识、无「为什么找我聊」入口(match_screen.dart:535-588 的 receiveReply 只对 match_reaction 做 deliveryKey 去重,:996-1008);未完话题台账只在运营台(console Threads 页),用户既看不到「上次没聊完的」也不能主动续;backchannel 进同一条音频 FIFO 纯音频消费、与正式回复不可分辨(match_session_controller.dart:1072-1090)。竞品证据:Character.ai 记忆改版被社区列为最重大更新且是付费点(Pin to Memory/Facts 可见可改可删);星野事件簿(阶段摘要+用户可编辑)实测记忆连贯性 +60%。

同时,Open Thread 存在双账本泄漏:`memory.Thread`(open_threads 表,五类含 unroutable,召回补答消费)与 `relationship.MemoryKindOpenThread`(StateBundle 内,ActRecall 消费)marker 词表不同、关闭语义不同(companion/thread_observers.go:88-124 用 PromiseMarkers;relationship/memory.go:31-40, 214-227 用「下场接着聊」等词表 + PendingDecisionIDs)——同一次「用户留了个话头」可能两边各记一条,「哪个是权威」无答案。

## What Changes

- **A1 · 账本统一**:memory.Thread 成为 Open Thread 唯一权威;relationship 侧 MemoryKindOpenThread 改存 thread ID 引用(不再自记内容);两套 marker 检测词表合并为 companion 检测层单一源(PromiseMarkers 收编 relationship 侧词表);关闭语义统一为 MarkThreadAddressed(投递成功后关闭,failed 不关——沿用 post-match beat 的正确先例);存量两本账去重合并(同用户同内容留 Thread 版)。
- **B1 面1 ·「球球记得的事」**:画像页(portrait_screen.dart 已趟平可见/可编辑/可删隐私生命周期)扩展「共同瞬间」段——Shared Moment 列表(来自 Memobase events),单条可「忘掉」(tombstone 语义复用)。**不展示原始 Interaction Ledger 行**——那是运营观测域(Operations Observation 纪律),用户面只给记忆与共同瞬间。
- **B1 面2 · 主动回合理由入口**:主动回合下发 wire 增可选 `reason` 字段(policy reason code / `proactive_citation:<code>`);客户端主动回合卡片带「为什么找我聊」入口,展开显示理由与引用(提醒/订阅/赛点/共同瞬间)。可选字段,老客户端零破坏。
- **B1 面3 · 话题条**:新端点 `GET /api/me/threads`(session bearer 鉴权,同 /api/me/portrait 口径)返回用户未完话题列表;客户端「上次没聊完的…」条,点击即走既有 thread recovery 路径续聊。
- **B1 面4 · backchannel 字幕标注**:voice_audio 已带 `source:"backchannel"`(快修轮已确认)——字幕渲染按 source 微标注(视觉样式区分,不改 FIFO 配对)。

## User Stories

1. As a 用户, I want 看到「球球记得的事」并能单条忘掉, so that 信任她真的记得、且隐私在我手里。
2. As a 用户, I want 知道球球为什么突然找我聊, so that 主动回合是「有理由的陪伴」不是「机器人乱插话」。
3. As a 用户, I want 看到上次没聊完的话题并一键续上, so that 关系有连续性。

## Non-goals

- 球友手记/赛季记忆册(teammate-journal 独立 change,后置)。
- 球球预测+对账(B4——立项挂起,锚点=第一批真实陪看用户出现,见 teammate-journal Non-goals 记录)。
- 理由码回填历史回合(只对新回合生效)。
- 运营台改动(console Threads 台账已存在;本 change 只加用户侧)。
- 记忆高光多媒体(照片/视频卡片)——文本先行。

## Success Criteria

- 真机四张面全部可见可操作(记得页忘掉/理由展开/话题续聊/backchannel 标注);
- A1 合并后:同话头只存在一条 Thread;recall 补答与 ActRecall 消费同一实体;evals 话题套件(关闭语义/五类/引用)绿;
- wire 契约测试:`reason` 字段可选性、老客户端不破坏;
- 全量 go test + client flutter test + console vitest 绿;P0-1 真机验证先行(视觉面依赖形象上屏)。
