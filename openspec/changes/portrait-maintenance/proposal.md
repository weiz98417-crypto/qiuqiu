# Portrait Maintenance: 画像的冲突消解、失效与离线巩固

## Why

Reflection→Portrait 只累积不修证：`portrait_overlays`（migration 041）只有 `deleted` 墓碑、无时间有效字段（portrait_postgres.go:34-92 同构）；用户新旧主张冲突无消解机制——换主队后球球还替旧队说话；过期偏好不衰减、长期挂着。CONTEXT.md 对 Portrait 的要求是「织进措辞、不许装饰」，不修证的画像会把过期内容继续织进回合。外部参照（只抄机制、不引库，全 Apache-2.0 且活跃）：mem0 的 ADD/UPDATE/DELETE/NOOP 冲突操作集、Graphiti 的双时态事实失效（valid/invalid）、Letta 的 sleep-time compute 离线巩固。

## What Changes

**阶段一（冲突消解 + 时间窗）**：
- Portrait 更新器引入冲突操作集：新主张 vs 现存同 topic 条目 → 判定关系（走 structured seam 的 LLM 抽取）→ 确定性落地 ADD / UPDATE / DELETE / NOOP。
- `portrait_overlays` 加 `valid_from` / `valid_to`（nullable，NULL=全域有效），migration 编号顺延；读取侧时间窗查询。
- 墓碑保留、**不物理删**：被顶替/失效条目留在库里（valid_to 封口）——「2026-09 前用户支持的是 A 队」要可回放，这是账本可解释性精神在画像侧的延伸。

**阶段二（sleep-time 巩固，阶段一验收后）**：
- 挂既有 Reflection beat 尾部，不新增节奏：离线合并同 topic 近重复、衰减长期未确认条目（衰减=软置 valid_to，非删除）。

## User Stories

1. As a 用户, I want 球球记住我换了主队, so that 它不再替我早就放弃的队说话。
2. As a 用户, I want 半年前的临时偏好自然淡出, so that 画像跟得上现在的我。
3. As a 可解释性要求, I want 过期画像可回放, so that 「你为什么当时那么说」有答案。
4. As a Reflection 维护者, I want 冲突判定走 structured seam, so that 操作集不另抄一套 LLM 管道。

## Non-goals

- 换记忆库（Memobase seam 不动，ADR-0006）。
- 记忆激活侧重排/调参（heuristic 词表不动）。
- 用户可见的记忆编辑 UI（附录 B 产品 backlog，另议）。
- Memobase 侧的对应机制（portrait 修证在本仓；Memobase 黑盒不动）。

## Success Criteria

- 新记忆 eval：过期偏好不再出现在措辞；矛盾主张按操作集消解（UPDATE 场景旧条目封口、新条目生效）。
- 时间窗查询正确（NULL=全域语义、窗外交互不取）。
- 删除测试：删操作集则 Reflection 回到盲 ADD——冲突知识集中一处，是加深。
- 阶段一全部验收后 5.4 才动工（显式门）。
