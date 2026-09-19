# Tasks: Server Surface Split

- [x] 1.1 trace 辅助上提出 main.go（随比赛 API 入 match_operator_api.go，包内共享）；main.go 不再持有 console 依赖的读取辅助。
- [x] 1.2 比赛 API 25-case switch 提取为 match_operator_api.go（含五层构造器与私有辅助）。
- [ ] 1.3 路由注册表化——**部分收缩**：39 处 gate 已经由两个具名方法（`authorize`=401 失败 / `view`=公开降级）承载约定，本次未重构为表驱动；在 ADR-0011 冻结边界上做 switch 重写的风险大于收益，留作独立后续变更。
- [x] 1.4a 构造器洋葱塌缩（中间三层删除，测试调用点改走 WithOperatorAuth）；handleMatchAPI 保留为测试便利入口。
- [ ] 1.4b store 装配表驱动——**收缩**：15 段装配块各自带不同的构造器/选项/关闭器，强行抽象会遮蔽差异且触碰 DB 装配高风险区，维持显式写法。
- [x] 1.5 /ws/match/ 引擎提取为 watchconnection.go：闭包捕获面显式化为 watchDeps（11 个依赖），main() 只装配。
- [x] 1.6 interruptionRing 注入（newInterruptedReactionGuard(ring)，nil 回退全局默认）；submittedUserSignals 仍为包级（语音路径多处引用，注入需贯穿多层签名，留待后续）。
- [x] 1.7 验证：go test 全绿 + 28 条 operator-control evals 全绿 + console-director e2e 绿。

## Sequencing

第七个执行：化石已清（quick-wins）后搬移面最小；风险最高的 WS 提取放本变更末步。
