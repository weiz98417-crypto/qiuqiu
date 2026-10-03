# 0024 · 数据源自动托管与信任分级确认(auto-hosting)

日期:2026-10-04 · 变更:openspec/changes/auto-hosting · 状态:已接受 · 修订:ADR-0002

## 背景

ADR-0002 立下的门是「外部数据源只能提交 provisional 事实,运营动作 confirm/reconcile/revoke」——运营确认是事实可见的唯一入口。真实联赛托管的现实是:api-sports 集成已完整(轮询/游标/幂等入 provisional),但一场 90 分钟的比赛会产生几十条 provisional,逐条人工确认让「无人值守托管」不成立,运营员退化为确认按钮。同时上游改判盲区已修(datasource 漂移检测 + VAR 结果细分 + goal_cancelled 引用链),改判在账本里的正确表达是 correction 追加回放——自动确认不再是「把可能错的东西钉死」。

## 决定

1. **确认权扩为双通道(本 ADR 对 ADR-0002 的唯一修订)**:provisional → confirmed 的迁移由「仅运营」扩为「运营 或 信任分级自动确认」。其余一切不变:外部数据只能进 provisional、账本只进不改、快照不可 patch、LLM 永不生成比赛事实、冲突永不自动裁决。
2. **稳定窗(按时间,不按轮数)**:provisional 事实停留满稳定窗(默认 30s,env `QIUQIU_AUTOCONFIRM_WINDOW_SECONDS`,0=禁用)且窗内无上游漂移(poller 的 changed/retracted)→ 自动确认。窗按时间定义:双源轮询节拍不同(ESPN 秒级、api-sports 15-60s),按轮数不可迁移。窗内出现漂移即重置窗口。30s 的依据:自助数据档进球延迟 15-60s + VAR 改判高发窗;首窗保守,真实数据后调。
3. **事件类信任表(default-deny)**:仅 goal / red_card / yellow_card / substitution / kickoff / halftime 允许自动确认;var_check / var_result / goal_cancelled / score_correction / penalty / penalty_awarded 及其余一切类型永远留运营——改判与 VAR 敏感类是话术敏感事件,人工把关;表硬编码于 datasource 包,改动须过评审。
4. **自动确认的操作者显名**:operator 记 `auto:stability` 前缀标识,审计可追溯、绝不冒充人工;确认失败(账本拒绝)只记日志不重试当轮,sweeper 下轮重见重试。
5. **自动确认 ≠ 不可逆**:账本 append-only 语义不变;自动确认后上游改判,以 goal_cancelled/score_correction 追加(provisional)+运营确认回放——事件溯源架构本就为此设计。
6. **冲突挂起永不自动裁决**:跨源冲突(含双源仲裁挂起)一律留运营;漂移计数(SourceStatus.Drifts/Retractions)是运营台显形面。

## 后果

- 运营员从逐条确认解放为一键托管 + 异常裁决;真实比赛无人值守托管成立。
- ADR-0002 的「运营动作确认」表述由本 ADR 修订;用户面无感(自动确认与人工确认产物同形状,`IsPublicFact` 同判)。
- 信任表与稳定窗是安全阀而非产品开关:出现一次「自动确认了随后被取消的进球」即应收紧窗口或下调信任表,并把案例记回本 ADR 附录。
