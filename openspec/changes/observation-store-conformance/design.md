# Design: Observation Store Conformance

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | reducer 为纯函数（无 IO、无 store 依赖），两个 adapter 的 Record 前置阶段调用。 |
| 2 | conformance 测试：同一操作序列分别在 MemoryCoordinator 与 PostgresCoordinator（DATABASE_URL 自跳过，仓库惯例）上执行，断言最终观察集合一致。 |
| 3 | postgres.go 的 SQL/行布局不变。 |

## Seam

- reducer 纯函数即测试面；conformance 测试锁两个 adapter 的行为等价。

## Testing decisions

- 表驱动序列：重复观察、窗口过期、5 条挤出、冲突状态转移各一组。
- 先例：postgres 集成测试 self-skip 模式；applyFact 共享测试。