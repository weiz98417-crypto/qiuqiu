# 部署运维备忘

## postgres_data 备份与恢复

`postgres_data` 卷是唯一的全量事实源：画像、事实账本（Interaction Ledger）与全部
业务表都在里面，必须可恢复。备份用 `pg_dump`（custom 格式，可并行恢复、可压缩）：

```bash
# 手动备份（在仓库根目录，compose 项目内执行）
mkdir -p backups
docker compose exec -T postgres pg_dump -U qiuqiu -d qiuqiu -Fc > backups/qiuqiu-$(date +%F).dump
```

### 定时备份（crontab -e，每日 03:30）

```cron
30 3 * * * cd /path/to/qiuqiu && docker compose exec -T postgres pg_dump -U qiuqiu -d qiuqiu -Fc > backups/qiuqiu-$(date +\%F).dump
```

注意：crontab 里 `%` 必须转义为 `\%`。建议额外把 `backups/` 同步到宿主机之外的存储。

### 恢复步骤

```bash
# 1) 数据卷完好、仅回滚数据时：直接覆盖恢复
docker compose exec -T postgres pg_restore -U qiuqiu -d qiuqiu --clean --if-exists < backups/qiuqiu-YYYY-MM-DD.dump

# 2) 卷损坏需重建时：
docker compose down
docker volume rm qiuqiu_postgres_data    # 卷名前缀 = compose 项目名，docker volume ls 确认
docker compose up -d postgres            # 等待 healthcheck 通过（pg_isready）
docker compose exec -T postgres pg_restore -U qiuqiu -d qiuqiu < backups/qiuqiu-YYYY-MM-DD.dump
docker compose up -d
```

Memobase 的 `memobase_postgres_data`（画像缓存，可由账本重建）同理可备份：
`pg_dump -U memobase -d memobase`；丢失后果可接受，不强制定时。

## 向量数据迁移与再生（semantic-memory）

`embedding_moments` 表存的是 Moment 文本的 bge-m3 向量（语义召回缓存），
**不进 git**。跨机器两种做法：

1. 默认：不迁移。新机 `docker compose up` 后迁移脚本自动建表（046），
   配好 `EMBEDDING_BASE_URL`（本地 Ollama bge-m3）后向量随新对话重新积累；
   积累前语义召回自动降级为关键词路，功能不受影响。
2. 要带走已积累的向量（换机前提：两端同为 bge-m3/1024 维，否则向量不可比）：

   ```bash
   pg_dump qiuqiu -t embedding_moments > vectors.sql   # 旧机导出
   psql "$DATABASE_URL" -f vectors.sql                 # 新机导入
   ```

知识条目（backend/knowledge/*.yaml）是 repo 纯文本，随 git 走，零迁移。
