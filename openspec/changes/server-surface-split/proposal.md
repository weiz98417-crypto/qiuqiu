# Server Surface Split: main.go 拆出两块最大的面

## Why

backend/cmd/server/main.go 3,063 行：/ws/match/ 实时会话引擎是写在 main() 里的 675 行内联闭包（15-case 读循环、投递跟踪、主动调度、心跳）；比赛运营 API 是一个 765 行单函数里的 25-case switch（20 处手写 scope gate）；~15 个 store 各有一段手写「memory 或 postgres」装配块；handleMatchAPI 五层构造器洋葱只剩历史价值。干净的 console_api.go 反而要反向依赖 main.go 的 listInteractionTraces。加一条路由至少碰 3 个文件；scope 的两种约定（401 失败 vs 公开降级）藏在每个 case 第一行，下一条新路由选错约定没有信号。

## What Changes

- /ws/match/ 引擎提取为独立编译单元（显式 deps struct；15-case 读循环、心跳、主动调度随之搬家）。
- 比赛 API 的 25-case switch 提取为独立编译单元——它是 ADR-0011 冻结形状的宿主，模块归属即退役边界。
- store 装配表驱动：一个辅助函数吃 (postgres, memory) 对，15 段手写块归一。
- 构造器洋葱塌缩为单层；variadic 遗迹删除。
- 路由注册表化：每条路由一行（method、path、scope 约定：401 失败或公开降级），两种约定在注册处肉眼可见。
- console_api.go 对 main.go 的反向依赖（listInteractionTraces 等）上提到共享文件。
- 包级全局（interruption ring、submittedUserSignals）改构造注入。

## User Stories

1. As a 后端维护者, I want 加一条 console 路由不碰 main.go, so that 变更范围最小化。
2. As a 比赛运营 API 维护者, I want 25-case switch 是一个文件的 implementation, so that ADR-0011 冻结面有明确归属。
3. As a 新人, I want 读 main() 只见装配, so that 十分钟能定位业务代码在哪。
4. As a WS 引擎维护者, I want 会话引擎 deps 显式注入, so that 单测不再需要整个服务器。
5. As a 路由作者, I want scope 约定在注册行上可见, so that 「这条该公开降级还是 401」不再靠猜。
6. As a console 测试作者, I want 测试 harness 按需装配, so that 测一条路由不用 9 件套。
7. As a 28 条 operator evals 的守卫, I want 搬移后 evals 全绿, so that 纯搬移的承诺被证明。

## Non-goals

- 零行为变化：请求形状冻结（ADR-0011）、路由语义、scope 判定结果全部不动。
- 不做目录级大重构（包结构不变，仍是 cmd/server 内的文件拆分）。
- 不引入路由框架（stdlib mux + 注册表即可，零依赖纪律）。
