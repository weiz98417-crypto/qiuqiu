# Tasks: Console Contract Goldens

- [x] 1.1 golden 测试辅助（构造固定输入 + 归一化 + 对比 + regenerate 模式）。
- [x] 1.2 /api/matches/:id/{events,clock,config,state} golden 化。
- [x] 1.3 /api/console/* 全端点 golden 化（TestConsoleOverviewShape 迁移为文件对比）。
- [x] 1.4 evals.yml 增加 console 构建步骤（npm ci + tsc && vite build）。
- [x] 1.5 验证：go test 绿 + CI 全链路绿。
- [x] 1.6（退役时执行）event-model parity 锚点迁移至 golden。

## Sequencing

第四个执行：退役门之前必须落地——它给退役签署提供可执行证据。
