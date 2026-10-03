# 自动托管:真实联赛数据接入与信任分级确认

## Why

球球现在的事实入口是运营员手工导演(杯赛/demo 场),接真实联赛是「第一批真实陪看用户」的前置。走查发现**自动托管的一半已经建好**:api-sports 完整集成(客户端/3 秒轮询/游标断点续传/按 ProviderEventID 幂等入账——datasource/poller.go:48, manager.go:311-341, 482)、事件自动流入账本但全部 provisional(公开快照只认 confirmed/reconciled,matchstate/store.go:1363-1378)、跨源冲突自动隔离已建(store.go:650-673)、运营台 /sources/start|stop|takeover 已有。缺口四个:

1. **自动确认不存在**——ConfirmFact 唯一调用点是运营 API(match_operator_api.go:702-704);ADR-0002 的门是「运营确认」。
2. **poller 改判盲区**——只处理「ID > 游标」的新事件(poller.go:128-134),无内容 diff、无撤回检测;`Var` 一律粗映射 `var_check` 不分结果(poller.go:210-226)。上游改判(VAR 取消进球/比分回滚)在本地无任何动作。
3. **改判重放语义已建但无人能自动产生**——投影引擎支持 goal_cancelled(回退比分)与 score_correction 的重放(fact_ledger.go:171-212),但这两种事件类型目前只能由运营路径产生。
4. **数据源经济性**——api-sports 免费档 100 次/天不够直播轮询(一场 90 分钟 15s 一轮=360 次);ESPN hidden API 免费无 key、秒级、整届 2026 世界杯实战验证,且 demo-seed.mjs:230-231 已在用其 summary 端点(历史);但非官方无 SLA。

生态调研结论:没有现成「实时比分→事件流」开源项目;爬虫路线(SofaScore/FotMob)WAF/takedown 实锤不做;GOAL API 把 `goal.scored` 与 `score.changed` 设计成两种 webhook——官方承认改判是常态,这正是需要的事件分类学。

## What Changes

- **改判盲区修复(纯工程前置,不动宪法)**:poller 增事件内容 diff(同 ID 内容变化检测);VAR 结果细分——改判产生 `goal_cancelled` / `score_correction` 事件(provisional 级,重放语义走既有引擎);上游事件消失(撤回)映射为待裁决冲突而非静默。
- **ESPN live adapter**:datasource.Manager 增 `SourceESPN` 类型(manager.go:19-23 枚举扩展)——scoreboard 快照轮询 + 前后快照 diff 产出事件(与 api-sports 的事件游标模型并存,两种 source 形态);live 场次加速节拍、非 live 慢节拍。ESPN key-free、无 SLA 的风险由下一条对冲。
- **双源仲裁**:ESPN × api-sports 对同一比赛同类事实不一致 → 事件挂起、该事实降级「比分核对中」(复用跨源冲突隔离机制 store.go:650-673),一致则互为印证。
- **ADR-0024 · 信任分级确认**(修订 ADR-0002 的运营确认步骤):稳定窗自动确认——事件在 provisional 停留满**时间窗**(默认 ~30s,按时间不按轮数,两源节拍不同)且期间无 diff 变更、无跨源冲突 → 自动 confirm(操作者记 source 名);**事件类信任表**:goal/red_card/substitution 等稳定类可自动,VAR 进行中/`score_correction`/`goal_cancelled` 永远留运营确认。账本 append-only:自动确认不是不可逆,改判以 correction 追加回放——事件溯源架构的红利。
- **一键托管此场**:运营台从今日赛程(schedule reader 已有)选场 → 建场 + 开场 + 挂源 + 订阅展开联动(subscription_expansion 已消费同一赛程);不做自动扫全场建场(ADR-0023)。
- **data-provider-lite-bridge 收编关闭**:有用任务(fixtures/元数据校验)并入本 change,其余归档。

## Non-goals

- 自动扫全部 live 比赛自动建场(一键托管为止,ADR-0023 消费先于产能)。
- Sportradar/Opta/GOAL API 商务接入(远期;GOAL API 只作事件分类学参照)。
- SofaScore/FotMob/Flashscore 爬虫(法律与稳定性风险)。
- LLM 参与事实判定(宪法红线不变:确定性管道;LLM 只消费已确认事实)。
- directordraft 语音导演任何改动(与自动托管正交,继续服务杯赛/人工场)。

## Success Criteria

- 一场真实联赛比赛全程**无人值守**托管:开场→事件流入→自动确认→终场,运营台只看不打;
- 注入一次改判(测试或真实 VAR):账本正确回放(goal_cancelled 回退比分),用户侧话术不越权;
- ADR-0024 落库;稳定窗/信任表/provisional→confirm 时序有确定性 eval;
- 双源不一致场景:挂起+「比分核对中」降级正确;
- api-sports 集成保持可用(二源,付费开闸即用)。
