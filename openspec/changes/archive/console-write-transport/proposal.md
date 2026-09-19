# Console Write Transport: 导播台写路径的传输语义收进一个 seam

## Why

ADR-0011 四步重写把导播台迁入 React console，但写路径的传输语义没有跟过来：旧 operator.html 的 inflight 去重（按 method:path:body 键）、5xx 单次重试、409 冲突专用提示、Idempotency-Replayed 响应头识别，在 console 的 `api()` 里全部缺失——只剩幂等键生成。导演双击「确认并发送」会生成两个不同的幂等键，落两条草稿事件。ADR-0011 的 parity harness 只对比事件模型 payload 的 byte-shape，看不见传输层的退化，而两代页面并存的窗口期恰是这层最需要保护的时期。

## What Changes

- `api()` 成为运营写请求的唯一 transport seam：inflight 去重、5xx 单次重试、409 冲突文案、replay 标记识别在此实现一次，全部写调用受益；语义照抄旧页已验证的行为，不做发明。
- 错误响应体解析为 JSON（现在是裸文本），409 的冲突消息取自响应体。
- 合并对同一端点 `GET /api/matches/:id/events` 的双封装（consoleApi.matchEvents 与 director loadEvents），类型统一为 director 的富类型 `DirectorEventRow`。
- director/api.ts 的端点定义并入 consoleApi 注册表，端点注册只剩一种风格。

## User Stories

1. As a 导演（director 角色运营员）, I want 双击「确认并发送」不产生重复事件, so that 比赛事实流不会被手抖污染。
2. As a 导演, I want 发布请求遇 5xx 自动重试一次, so that 偶发服务端抖动不需要我手动重发。
3. As a 导演, I want 提交冲突（409）时看到明确的冲突提示, so that 我知道该刷新而不是反复重试。
4. As a 导演, I want 界面能区分「新写入」与「幂等重放确认」（Idempotency-Replayed）, so that 我对事件是否已落库有把握。
5. As a console 维护者, I want 传输策略（去重/重试/冲突）只在一处实现, so that 修一次全部 12 个写调用同步受益。
6. As a console 维护者, I want 比赛事件列表只有一个函数一种类型, so that 不在两套注册风格里挑。
7. As a 回归守卫（CI）, I want transport 行为有无框架的单测覆盖（双击/重试/409/replay）, so that parity harness 的盲区有测试兜底。

## Non-goals

- 不改任何后端请求形状（ADR-0011 冻结基线）。
- 不引入 React Query 或服务端状态缓存层。
- 不动 401/刷新机制（评审确认已是单点实现，不需要动）。
