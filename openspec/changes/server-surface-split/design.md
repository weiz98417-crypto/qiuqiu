# Design: Server Surface Split

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | 纯搬移：函数体逐字搬进新文件（ws 引擎、比赛 API switch、trace 辅助），行为零变化；28 条 operator-control evals 是等价性的回归网。 |
| 2 | 路由注册表：`route{method, path, scope, degrade}` 的静态表 + 一个解释器替代散布的 authorize/view 首行调用；401 失败与公开降级是表里的显式值，不是每个 case 的隐式选择。 |
| 3 | store 装配：`openStore[T](pg func() (T, bool, error), mem func() T)` 风格的表驱动辅助，消除 15 段重复的 DATABASE_URL 分支。 |
| 4 | 构造器洋葱：保留最外层签名一处，中间四层删除；operator_write_api 的 variadic selectedOperatorWriteService 收敛单参数。 |
| 5 | 全局注入：interruption ring 与 submittedUserSignals 在 main() 构造，经 deps 传入消费方；测试不再替换包变量。 |
| 6 | console_api.go → main.go 的反向依赖（listInteractionTraces / getInteractionTrace / projectInteractionTraces）上提到共享 trace 文件，console_api 自包含。 |

## Seam

- 搬移不新增 seam；唯一新 seam 是路由注册表（一处声明全部路由与 scope 约定）——它的 interface 是表本身，测试断言注册齐全 + 约定正确。

## Testing decisions

- 等价证明：go test 全绿 + pr 档 evals 全绿（28 条 operator-control 驱动旧页打 /api/matches/*，是冻结面的直接回归网）。
- 注册表测试：每条已注册路由的 scope 约定与搬移前逐一相同（对照 git diff 人工核 + 表快照测试）。
- 不新增行为测试（行为未变）。

## Further notes

- WS 闭包捕获的变量清单在搬移前先盘出来（deps struct 的字段来源）；这是本变更风险最高的单步，放最后、独立验证。
