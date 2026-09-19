# Design: Client Resilience Quickfixes

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | connected 分支 unawaited(_loadMatchOverview())——幂等 GET，失败静默（快照通道会再补）。 |
| 2 | release 构建（kReleaseMode）缺 matchId / WS URL → 空态错误页提示配置缺失；debug 保持 'test' / 10.0.2.2 默认。 |
| 3 | shouldAutoEnterMatch 删除，调用点内联 false 并注释保留理由。 |

## Seam

- MatchViewData / 状态分支即测试面：connected 触发概览重拉的判定抽纯函数可单测；release 缺失空态用 widget 测试。

## Testing decisions

- 新增：重连触发概览重载的判定单测；release 缺配置空态 widget 测试。
- 回归：flutter test 全绿。