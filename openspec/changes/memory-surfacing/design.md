# Design: 记忆可感面

## D1 · 账本权威选 memory.Thread(不是反过来)

裁决依据(grilling Q3):Thread 表已 PG 持久化、五类齐全含 unroutable、召回补答路径已在消费、与 B1 话题条天然同源;StateBundle 是连接内状态、无独立持久化语义。relationship 侧保留的是「ActRecall 消费视图」:存 thread ID 引用,ActRecall 命中时经引用读 Thread 内容。marker 词表(「下场接着聊」「下次再说」等 relationship/memory.go:214-227 的词)并入 companion/thread_observers.go 的 PromiseMarkers 单一源——检测是 companion 的职责,记账是 memory 的职责,分开。

关闭语义统一为 MarkThreadAddressed + 投递成功回调:post-match beat 已经做对(AddressOpenThread 在投递落地后调),把这个时序立为唯一语义;relationship 侧 PendingDecisionIDs 路径退役。

存量迁移:一次性脚本——扫描 StateBundle 侧 MemoryKindOpenThread,同用户内容与 Thread 表匹配(精确/归一化匹配)则删 StateBundle 份留引用;无匹配则补录进 Thread 表再转引用。迁移幂等,可重跑。

## D2 ·「记得的事」不展示原始 Ledger

用户面 = Portrait(已有)+ 共同瞬间(Shared Moments)。Interaction Ledger 的原始行是运营观测域(Operations Observation:「单轮回放在用户详情页同特权下可见」是运营侧纪律),把账本流水直接翻给用户违反隐私分层。忘掉 = tombstone(画像页既有语义),对共同瞬间同样适用(侧记 tombstone 进 overlay,recall 时过滤)。

**共同瞬间数据源二选一(实施时定)**:Memobase events 时间线投影(持久化在 Memobase,与忘掉语义天然衔接)vs relationship.MemoryKindSharedMoment(StateBundle 内,与 ActRecall 同源)。判据:哪个有独立持久化与分页能力——倾向前者;若选后者须先补持久化(类似 A1 对 Thread 的裁决,但 SharedMoment 无双账本问题,走查只确认了 Open Thread 双账本)。

## D3 · reason 字段 wire 形状

主动回合与 match_reaction 走同一 qiuqiu_reply 家族事件;增可选 `reason`(对象:`{code, citation?, label}`)。code 取 policy reason codes / `proactive_citation:*`(trace 已有,零新语义);label 为中文短句(「你订的开球提醒」「这是你关心的球队」)。契约测试锁:字段缺席时客户端行为与现状逐字节一致。

## D4 · /api/me/threads 鉴权与隐私

同 /api/me/portrait:session bearer、账号 scoped、匿名身份走设备标识会话。只返回该用户的未关闭话题(内容 + kind + 时间),不含 match_id 以外的比赛细节;删除/关闭操作复用既有 recovery 投递路径,不新开写口。

## D5 · backchannel 标注只动渲染

FIFO 配对、deliveryKey 去重、播放优先级全部不动;只在字幕组件按 `source` 字段切样式(弱化字号/浅色/「(小声)」前缀——具体样式真机调)。ADR-0016「伴随反应不是回合」语义不变。
