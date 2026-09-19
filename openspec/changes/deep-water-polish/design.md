# Design: Deep Water Polish

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | 词汇表收敛为行为等价搬移：词集逐字保留，分类器逻辑改为查表；director_test 841 行全绿即等价证明。 |
| 2 | Deliver 拆分：先在锁内完成全部状态判定与队列操作产出「投递计划」，锁外执行网络 IO——禁止跨 IO 持锁。 |
| 3 | directordraft 拆文件不动函数签名（同包内聚分离）。 |

## Seam

- policy_vocabulary 表即测试面；Deliver 的计划/执行两半各自可测（计划半纯函数化）。

## Testing decisions

- 等价证明：relationship / conversation / directordraft 既有测试全绿；新增 Deliver 计划半的纯函数单测。
- 回归：go test 全量 + evals。