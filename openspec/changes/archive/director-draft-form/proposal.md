# Director Draft Form: 草稿↔表单映射提取为纯 module

## Why

导播台重写（ADR-0011）产出了模范的 event-model.ts（纯函数 + byte-shape 锁），但它周边的适配逻辑困在 494 行的 DirectorLive.tsx 组件里：草稿→表单同步（注释自记 preserveForm 防覆盖 bug 史）、表单→草稿 9 步合并（含时钟正则与 mainPlayer 回填，「否则提交时静默丢失」）、事件行→草稿装载（含 `__quiet__` 哨兵解码）、服务端锚定的时钟插值。这些逻辑 interface 是 React 的——想测映射必须先渲染整页，目前仅有 3 条 Playwright e2e 覆盖。

## What Changes

- 提取 `director/draft-form.ts` 纯 module（event-model.ts 的姊妹）：syncFormFromDraft、submitDraft 合并、loadEventIntoDraft、currentClockElapsed 四组纯函数。
- DirectorLive.tsx 收缩为渲染 + 编排（setState、轮询等副作用留在组件）。
- 纯 module 纳入与 check-director-event-model.mjs 同款的无框架 harness 测试。

## User Stories

1. As a 导播台维护者, I want 映射逻辑有毫秒级单测, so that 改合并规则不用跑 60 秒的 e2e 才知道错没错。
2. As a 导播台维护者, I want 「用户正在编辑的描述不被草稿旧值覆盖」（preserveForm）语义在纯函数签名里显式存在, so that 这类 bug 的防线可测而不是靠注释提醒。
3. As a 导播台维护者, I want 时钟格式解析与 mainPlayer 回填可表驱动测试, so that 静默丢失类 bug 在提交前被拦。
4. As a 导播台维护者, I want 新旧页对齐验证可以直接 diff 纯函数输出, so that 退役签署清单有一份可执行的对照。
5. As a console 读者, I want DirectorLive.tsx 读起来是编排顺序, so that 500 行组件不再藏一个状态机。
6. As a CI, I want 纯 module 测试进 pr 档, so that 回归在提交层就被拦。

## Non-goals

- 不改任何映射行为（提取即等价，发现 bug 另行记录）。
- 不引入 vitest 等测试框架（零依赖纪律，沿用 esbuild + node:test 模式）。
- 不动 event-model.ts（它已是深 module 且有锁）。
