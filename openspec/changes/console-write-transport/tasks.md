# Tasks: Console Write Transport

- [x] 1.1 `api()` 增加 inflight 去重（method:path:body 键）与写方法幂等键生成保持兼容。
- [x] 1.2 5xx 单次重试；409 → 冲突专用错误（消息取 JSON body）；错误体统一 JSON 解析。
- [x] 1.3 `Idempotency-Replayed` 响应头识别并暴露给调用方。
- [x] 1.4 合并 `matchEvents` / `loadEvents` 双封装，类型统一 `DirectorEventRow`；director/api.ts 并入 consoleApi 注册风格。
- [x] 1.5 新增 transport 单测（node:test + esbuild bundle，零新依赖）：双击、重试、409、replay、GET 无键。
- [x] 1.6 验证：console 构建绿 + console-director evals 绿。

## Sequencing

八个候选中的第一个（现在就在丢行为，先止血）。
