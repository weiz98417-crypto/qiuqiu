# CI & Compose Hardening: 并发取消、缓存、依赖更新与生产 compose 加固

## Why

CI（evals.yml）已覆盖完整测试矩阵，但缺并发取消（同一 PR 连推会排队浪费）、Playwright 浏览器与 console 依赖无缓存、无 dependabot；docker-compose 里 backend 缺 postgres 的 service_healthy 依赖（启动即跑迁移会与首启赛跑）、MEMOBASE_POSTGRES_PASSWORD 有默认值、postgres_data 无备份方案说明（全部画像/事实都在该卷里）。

## What Changes

- evals.yml：concurrency（同分支取消旧跑）；actions/cache 缓存 Playwright 浏览器与 console npm ci（cache-dependency-path 指向 console/package-lock.json）；新增 .github/dependabot.yml（github-actions + npm + gomod 周检）。
- docker-compose.yml：postgres 加 healthcheck；backend depends_on 加 condition: service_healthy；MEMOBASE_POSTGRES_PASSWORD 去默认值改必填占位。
- deploy 文档：补 postgres_data 备份（pg_dump 定时）与恢复一段。

## User Stories

1. As a 提交者, I want 连推只跑最新一轮 CI, so that 不排队不浪费。
2. As a 运维者, I want 迁移不会与数据库首启赛跑, so that 首次部署不靠运气。
3. As a 运维者, I want 备份方案有文档, so that 画像与事实账本可恢复。

## Non-goals

- 不加部署 job（部署形态未定）。
- 不动 release 档与密钥注入方式。