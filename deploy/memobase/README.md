# Memobase（记忆综合服务）

ADR-0006 的分工：本地 Interaction Ledger 保持 append-only、可重放，是事实源；Memobase 只承载**可变的综合记忆**（用户画像、事件摘要）。适配器代码在 `backend/internal/memory`，只用 Go 标准库镜像官方 Go SDK 的 HTTP 调用（不给 `go.mod` 加依赖）。

## 服务拓扑（docker-compose.yml）

| 服务 | 镜像 | 说明 |
| --- | --- | --- |
| `memobase` | `ghcr.io/memodb-io/memobase:latest` | 容器内 8000，宿主机映射 `8019:8000`；配置挂载 `./deploy/memobase/config.yaml:/app/config.yaml` |
| `memobase-postgres` | `postgres:16-alpine` | Memobase 专用，卷 `memobase_postgres_data`，**不与球球的 pg 共用** |
| `memobase-redis` | `redis:7-alpine` | Memobase 专用，仅 compose 内网可达 |

## 环境变量

### 球球 backend（适配器客户端）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `MEMOBASE_URL` | `http://localhost:8019`（compose 内 `http://memobase:8000`） | Memobase API 地址，客户端拼接 `/api/v1` |
| `MEMOBASE_TOKEN` | 空 | 访问令牌（即服务端 `ACCESS_TOKEN`）；**为空 = 记忆层停用**，观察入队直接跳过、检索回退 `conversation.read_recent`，对用户不可见 |
| `MEMOBASE_EXTRACTION_TIMEOUT_MS` | `10000` | Memobase HTTP 超时（含 flush 的 LLM 提取等待） |

### memobase 服务端

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `ACCESS_TOKEN` | `secret` | 客户端用 `Authorization: Bearer <MEMOBASE_TOKEN>` 认证，生产必须改掉 |
| `MEMOBASE_LLM_API_KEY` | —（必填） | 提取 LLM 的 key，复用后端 `MIMO_API_KEY` |
| `MEMOBASE_LLM_BASE_URL` | `https://api.xiaomimimo.com/v1` | 提取 LLM 的 OpenAI 兼容地址，复用 `MIMO_BASE_URL` |
| `MEMOBASE_BEST_LLM_MODEL` | `mimo-v2.5-pro` | 提取模型，复用 `MIMO_MODEL` |
| `MEMOBASE_ENABLE_EVENT_EMBEDDING` | `false` | v1 关闭事件向量依赖（config.yaml 中同样为 false） |
| `MEMOBASE_POSTGRES_PASSWORD` | `memobase` | 专用 Postgres 密码（服务端与数据库容器共用） |
| `PROJECT_ID` | `qiuqiu` | Memobase 项目标识 |

`MEMOBASE_*` 前缀是 Memobase 官方的 env 覆盖约定（任意 config.yaml 键均可覆盖）。

## 行为要点

- **写入异步**：回合只写 Ledger；Moment 进 `backend/internal/memory` 的有界队列，后台插入 ChatBlob 并 flush（LLM 提取发生在服务端），重试指数退避，失败落本地 `memory_backlog` 表，恢复后回放。
- **重要性**：入队时由确定性启发式打分（`heuristic.go`），审计表 `memory_extraction_audit` 记录每次提取决策的 reason code；Memobase 综合只细化画像，不改写入分数。
- **降级**：连接失败/5xx ⇒ 适配器进入 degraded，`Recall` 返回空，agent 回退 `read_recent`；成功调用自动恢复。`MEMOBASE_TOKEN` 为空时整个记忆层静默停用。
- **反思节拍**：`cmd/server/main.go` 的 `runReflectionBeat`（赛后 + 空闲 ticker）触发 flush + 画像刷新，审计写 `memory_reflection_audit`，洞察引用账本序号。

## 本地验证

```bash
docker compose up -d memobase-postgres memobase-redis memobase
curl -H "Authorization: Bearer $MEMOBASE_TOKEN" http://localhost:8019/healthcheck
```
