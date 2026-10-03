# Tasks: 记忆可感面

- [ ] 1.1 A1 账本统一:relationship.MemoryKindOpenThread 改 thread ID 引用;PromiseMarkers 收编 relationship 侧 marker 词表(单一检测源);关闭语义统一 MarkThreadAddressed+投递成功时序;存量合并迁移脚本(幂等可重跑)。
- [ ] 1.2 A1 evals:话题套件——关闭语义(成功关/失败不关)、五类含 unroutable、双入口开线程只落一条、ActRecall 与召回补答同实体。
- [ ] 1.3 wire:主动回合/比赛事件下发增可选 reason 字段(code/citation/label);契约测试锁可选性(缺席=现状)。
- [ ] 1.4 面1 服务端:共同瞬间读取端点(画像页扩展;数据源按 design D2 二选一定稿——Memobase events 投影 vs relationship SharedMoment + tombstone 过滤)。
- [ ] 1.5 面1 客户端:画像页「共同瞬间」段 + 单条忘掉(tombstone 语义复用)。
- [ ] 1.6 面2 客户端:主动回合理由入口与展开卡片。
- [ ] 1.7 面3:`GET /api/me/threads`(session bearer)+ 客户端话题条与续聊动作(走既有 recovery)。
- [ ] 1.8 面4:backchannel 字幕按 source 微标注(仅渲染层)。
- [ ] 1.9 门禁:go 全量 + client 全量 + console vitest;P0-1 真机验证先行;四张面真机过一遍。

## Sequencing

波1A,与 auto-hosting(波1B)双线并行、代码面零重叠(本 change 客户端为主+记忆域,彼 change datasource+运营台)。前置:快修轮已清(P0-1 真机验证是本 change 验收的前置)。共同瞬间段的忘掉语义依赖 Memobase tombstone 能力——若 0.0.4x 版本不支持事件级 tombstone,降级为「整类忘掉」并在任务内记录。
