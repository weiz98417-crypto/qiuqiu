# 记忆打分收编 + 写回审查 + Memobase 版本核对

## Why

三件记忆工程债(调研 C4,Worth exploring):

1. **两代技术并存**:召回的 adapter 腿(占一半配额)仍是原始 contains 打分——0.5 基线+contains+新近(memobase.go:442-460),重要性固定;向量腿已是余弦+指数衰减(queue.go:493-519)。同一召回里两代算法混排。generative_agents 三因子(importance×recency×relevance,MIT)Go 几十行可补齐;importance 写入时已有启发式(heuristic.go:9-51),reflection 触发可从定时改**重要性累加阈值**(省无效反思调用)。
2. **写回无审查**:reflection beat 的 flush→权威层写回直接落;画像漂移(用户自述矛盾/抽取噪声)无闸。Letta sleep-time compute 与 MIRIX Auto-Dream 共同验证「写回前 dry-run 审查」模式——一次额外 LLM 调用换画像质量。
3. **Memobase 版本滞后待核对**:0.0.36 context API(记忆直接打包进 prompt,省一层胶水)/0.0.37 event gist 细粒度搜索/0.0.40 workflow 重写(LLM 调用固定 3 次,token -40~50%)——自托管(仓库根 mbcompose.json)升级即收益,但适配器是否用上待核对。

## What Changes

- **三因子收编**:adapter 腿 recallScore 升级——importance(写入时已有)参与打分、新近衰减对齐向量腿 τ;两条腿同代后融合配额语义不变。
- **反思触发改重要性累加**:近期记忆 importance 累计超阈值才触发 reflection beat(替代纯定时);定时兜底保留(低活跃用户)。
- **写回 dry-run 审查**:reflection 写回权威层前,一次轻量 LLM 自查(矛盾/噪声/越权改 fact 红线检查),不过审则记审计跳过;审查记录进 reflection 审计链。
- **Memobase 核对**:盘点 0.0.36/37/40 能力 vs 现适配器(gist 搜索/context API 未用则接入);升级自托管镜像,回归 evals。

## Non-goals

- 迁移 mem0(调研结论:与 Memobase 正交,迁移是降级)。
- 图记忆/时序知识图谱(Match Fact 已含更正历史,graphiti 不必引)。
- 记忆结构/schema 变化。

## Success Criteria

- 召回排序 eval:importance 参与后排序变化仅发生在「低相关高重要」记忆上位(方向性断言);
- 反思触发:同数据下触发次数不升、无效反思(零新洞察)次数降;
- dry-run:注入矛盾自述用例被拦,审计链可查;
- Memobase 升级后全量记忆 evals 绿 + token 消耗对照记录。
