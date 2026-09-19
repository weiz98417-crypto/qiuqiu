# Tasks: Quick Wins Cleanup

- [x] 1.1 删 `pipeline.ClassifyIntent`；核实并删 `generateProactiveText` 及 PromptManager / prompts v1.0 死链。
- [x] 1.2 llm client 移除内置人格；latency-test 显式传 system message。
- [x] 1.3 主动兜底罐头库合并为一处，删 main.go 副本。
- [x] 1.4 operatorauth 可选能力具名化；调用点删匿名探测与双签名 Delete。
- [x] 1.5 portrait view mapper + 错误映射收敛为一份。
- [x] 2.1 OperatorProvider 身份单源（解码一次 + whoami 兜底）；删渲染期解码。
- [x] 2.2 修登出幽灵路由跳转。
- [x] 2.3 TraceEvidence module（reason 码 + matchesCitation + Drawer）合并两页实现。
- [x] 2.4 状态标签并入 format.ts；事件词汇表单源化。
- [x] 2.5 删 ReactRouterLink（机械替换）与死导出。
- [x] 3.1 验证：go test 全绿 + console 构建绿 + pr 档 evals 绿。

## Sequencing

第二个执行：先删化石，缩小后续大搬移（agent 引擎提取 / main.go 拆分 / transport 合并）的搬移面。
