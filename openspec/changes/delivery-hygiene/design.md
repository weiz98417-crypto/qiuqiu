# Design: Delivery Hygiene

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | 根 .env.example 以 docker-compose.yml 的 environment 需求为清单来源；backend/.env.example 不动。 |
| 2 | VERSION 0.2.0.0；CHANGELOG [0.2.0] - 2026-09-20 汇总：意图路由器、JWT 登录、导播台重写与 operator.html 退役（ADR-0011/0013）、三轮架构与前端打磨、表演/记忆/身份修复。 |
| 3 | MetalButton：纯色 championBlue 填充 + 8px 圆角；替换后按 DESIGN.md 逐项核对。 |
| 4 | tests/unit 接入：run.mjs 增加 node --test tests/unit/ 步骤；widget_test.dart 文件改名（内容不动）。 |
| 5 | migrations lint 只查「数字前缀重复」，不改任何已应用文件名。 |

## Seam

- MetalButton 的 widget 即测试面（外观断言：无渐变、圆角值）；lint 脚本以 fixtures 自测。

## Testing decisions

- MetalButton：widget 测试断言 BoxDecoration 无 gradient、borderRadius 8。
- lint：对 tests fixture 目录跑通过/失败两例。
- 回归：flutter test 全绿 + run.mjs 本地全链路。