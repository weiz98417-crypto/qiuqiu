# Delivery Hygiene: 环境样例、版本账、设计对齐与孤儿测试

## Why

一批交付卫生问题：仓库根没有 compose 用的 .env 模板（README Docker 段复制 backend/.env.example 但它不含 ALLOWED_ORIGINS / POSTGRES_PASSWORD 等 compose 变量）；VERSION 冻结在 0.1.0.0、CHANGELOG 只有 7-15 一条，而 JWT 登录、导播台重写与退役、三轮架构升级都发生在其后；MetalButton 实测违反 DESIGN.md（三段渐变 + 12px 圆角 vs 「不使用渐变按钮」+ 8px）；tests/unit/*.mjs 四个文件从不被任何脚本运行（孤儿测试）；widget_test.dart 名不副实（内容全是纯 Dart 测试）；migrations 目录存在重复序号（010/011/012 各两份）无 lint 防再犯。

## What Changes

- 新增根 .env.example（docker-compose 变量全集：APP_TOKEN / SESSION_SIGNING_KEY / QIUQIU_JWT_SECRET / ALLOWED_ORIGINS / POSTGRES_PASSWORD / MIMO_API_KEY / MEMOBASE_* 等）；backend/.env.example 保持本地 dev 用途。
- CHANGELOG 增 [0.2.0] 段（三轮工作汇总）；VERSION → 0.2.0.0；README 的 APP_TOKEN 措辞与 JWT 现实对齐（两段互相矛盾的段落统一）。
- MetalButton 改纯色 championBlue + 8px 圆角（DESIGN.md 对齐）。
- tests/unit 接入 run.mjs（node --test tests/unit）；widget_test.dart 改名 match_view_data_test.dart。
- 新增 scripts/lint-migrations.mjs：重复数字前缀即失败（已应用的文件不改名）。

## User Stories

1. As a 新运营/新开发者, I want cp .env.example .env 一步可用, so that 上手不被第一步卡死。
2. As a 项目干系人, I want CHANGELOG 反映真实进展, so that 版本账可信。
3. As a 设计审阅者, I want 按钮符合 DESIGN.md, so that 视觉体系一致。
4. As a CI, I want 孤儿测试运行起来、迁移序号受 lint 保护, so that 测试与纪律不是摆设。

## Non-goals

- console bundle 代码分割（内部工具，出现加载投诉再议）。
- dependabot / concurrency（归 ci-compose-hardening）。