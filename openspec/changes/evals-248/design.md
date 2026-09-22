# Design: Evals 248

## 基建

- **awaitTurn helper**（tests/evals 共享）：`awaitTurn(page, {matchID, expect: 'delivered'|'silence', timeout})`——先轮询运营 API `/api/operator/...`（账本/投递结果端点，实施时确认具体路由）出现目标 Watch Turn/Delivery Outcome，再断言 UI。固定 `waitForTimeout` 全量替换。
- **真网档**：`tests/evals/router-net.spec.mjs`，`test.skip(!process.env.QIUQIU_ROUTER_NET, ...)`；15 例覆盖 11 意图分布+边界（错别字/口语化/域外）。
- **judge 离线档**：`scripts/evals/judge.mjs`——对指定 spec 的回复跑 LLM 评分（陪伴语感/织写自然度 1-5），输出 markdown 报告落 artifacts/，永不进 `npm test` 路径。

## 目录

`evals/cases/` 现有 baseline/boundary/regression 三目录不动；新域按 `evals/cases/<domain>/`（safety/persona/memory/companion/knowledge/multi/robust/voice/identity）分目录，runner 按目录聚合计数——248 的账目可对表。

## 计数纪律

全套资料口径（36/38/26=100）在 G 全部落地后一次性更新为新真值（按域分列），不做中间态。

## 素材流程

AnySearch 检索 → 汉化+足球语境改写（保留负例结构）→ 人工过一遍（用户抽查即可）→ 入库带 `source:` 注记。
