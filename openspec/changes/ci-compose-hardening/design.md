# Design: CI & Compose Hardening

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | concurrency: { group: evals-${{ github.ref }}, cancel-in-progress: true }。 |
| 2 | 浏览器缓存 key 绑 playwright 版本；console npm 缓存以 console/package-lock.json 为 dependency-path。 |
| 3 | compose 健康检查用 pg_isready；MEMOBASE_POSTGRES_PASSWORD 无默认（${MEMOBASE_POSTGRES_PASSWORD:?required}）。 |
| 4 | 备份：文档化 pg_dump 定时任务示例 + 恢复步骤（deploy/README 或 compose 注释块）。 |

## Seam

- 纯配置变更，无代码 seam。

## Testing decisions

- 验证 = actionlint（若有）/ YAML 解析 + 本地 docker compose config 语法通过；CI 行为以下一次真实推送观察。