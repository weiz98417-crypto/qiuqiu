# Design: Memory Recall Fusion

## 三路配额

```
adapter 路配额 = vectorPath 与 contentPath 均非空 ? limit/2 : （仅一路非空 ? limit - limit/3 : limit）
```

简化版（v1 采用）：保持现有「adapter 与向量路各半」不变，精确腿从向量路配额内出——向量路取 `limit/2` 条候选，精确腿命中者直接置顶替换向量路前部（同 content 去重）。效果：不引入第三种配额组合的测试矩阵，精确命中优先。

## 衰减

`weight = cosine * exp(-age/τ)`，τ=14d，age=now-occurred_at。只重排向量路内部顺序（adapter 路保序不动——它的排序是 contains 打分，语义不同）。τ 配置进 config（`memory.recall_decay_days`，默认 14，0=关）。

## SQL

```sql
SELECT content, importance, occurred_at FROM embedding_moments
WHERE user_id=$1 AND content ILIKE '%'||$2||'%' ESCAPE '\'
ORDER BY occurred_at DESC LIMIT $3
```

pattern 转义 `%_\`。超时与向量路共用 vectorQueryTimeout（200ms）预算外单独计时，超时弃权。
