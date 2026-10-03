# Tasks: 自动托管

- [ ] 2.1 poller 改判盲区:事件内容 diff + 撤回→待裁决冲突 + Var 结果细分(goal_cancelled/score_correction 产 provisional);evals:diff 三族(变化/消失/改判)。
- [ ] 2.2 ADR-0024 落库(信任分级确认:稳定窗+事件类信任表,修订 ADR-0002 确认步骤;含 D3 表与 D1 窗口依据)。
- [ ] 2.3 稳定窗自动确认:provisional 计时器 + 窗口内 diff/冲突否决 + 自动 confirm(操作者记 source 名);evals:时序(窗口内变更→不确认/窗口满→确认/冲突挂起→不确认)。
- [ ] 2.4 ESPN live adapter:SourceESPN + scoreboard 快照 diff(五类事件)+ ProviderEventID `espn:` 前缀幂等 + live/非 live 双节拍 + 健康探测;evals:快照 diff 用例族 + 冷启动重放幂等。
- [ ] 2.5 双源仲裁:ESPN×api-sports 同类事实不一致→挂起+「比分核对中」降级;一致互印证;evals:冲突注入。
- [ ] 2.6 一键托管此场:运营台今日赛程选场→建场+start+挂源+订阅即时展开(executeOperatorWrite 幂等);终场自动停源。
- [ ] 2.7 data-provider-lite-bridge 收编:有用任务(fixtures/元数据)并入后整目录 git mv 至 archive/。
- [ ] 2.8 soak:一场真实联赛比赛全程无人值守托管记录(时序/改判/仲裁/自动确认计数);运营台观测面补「托管场次」卡片(可选,数据走既有 overview)。

## Sequencing

波1B,与 memory-surfacing(波1A)双线并行零重叠。依赖序:2.1→2.3(盲区不修,自动确认不安全);2.2 在 2.3 前落库;2.4→2.5;2.6 依赖 2.4。真实比赛流量同时服务 smart-turn-v32(波3 影子对比评测素材)与 B4 挂起锚点判定(第一批真实陪看用户)。api-sports 免费档跑不动直播属预期——二源仲裁在 key 未配时降级单源,链路不依赖付费。
