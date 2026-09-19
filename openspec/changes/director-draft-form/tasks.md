# Tasks: Director Draft Form

- [x] 1.1 提取 syncFormFromDraft（preserveForm 进签名）。
- [x] 1.2 提取 submitDraft 合并（时钟正则 + mainPlayer 回填）。
- [x] 1.3 提取 loadEventIntoDraft（含 `__quiet__` 哨兵解码）与 currentClockElapsed。
- [x] 1.4 DirectorLive.tsx 收缩为渲染 + 编排。
- [x] 1.5 draft-form harness 测试（表驱动，覆盖注释里记过 bug 的每类场景）。
- [x] 1.6 验证：console 构建 + console-director e2e 绿。

## Sequencing

第三个执行：在 transport 修复与化石清理之后，契约锁（console-contract-goldens）之前。
