# Design: Director Draft Form

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | draft-form.ts 零 React import：输入（draft / form state / 事件行 / 时钟锚点）→ 输出（新 form / 新 draft / 插值时钟）全部纯函数。 |
| 2 | preserveForm 语义进签名：同步函数显式接收「当前编辑中的字段」并保留，而非依赖调用时机的隐式约定。 |
| 3 | 组件只留副作用：setState、1s 轮询 tick、事件订阅、提交调用。 |
| 4 | 测试模式复制 check-director-event-model.mjs 已验证的路径：esbuild bundle + node:test，零新依赖；测试进 CI pr 档（console 构建步骤由 console-contract-goldens 变更引入，本变更的测试随 node 测试目录运行）。 |

## Seam

- seam 位置：draft-form module 的函数 interface。测试只通过函数调用驱动，不渲染组件——interface 即测试面。

## Testing decisions

- 表驱动用例：时钟正则（合法/越界/缺位）、mainPlayer 回填、`__quiet__` 解码、preserveForm 字段保留、提交合并的 9 步顺序结果。
- 先例：event-model 的 byte-shape 对比脚本；companion 的纯谓词测试（TestIsEventClaimColloquialVariants）。
- 回归门：console 构建 + 既有 3 条 console-director e2e 绿（等价性证明）。

## Further notes

- 提取中若发现与旧页 operator-live-state.js 的行为差异，记录进 ADR-0011 的对齐清单，不在本变更内修。
