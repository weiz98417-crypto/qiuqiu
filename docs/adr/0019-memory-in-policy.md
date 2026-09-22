# 0019 · 决策层的记忆信号：口味放大与可解释偏置

日期：2026-09-22（第二波后续，openspec/changes/memory-in-policy）

## 背景

能力波 #2 把记忆限制在措辞层（引用码候选 + realizer 注入），决策层（沟通动作与 affect）看不见长期记忆。后果：支持的球队丢了球，决策层一视同仁地播报——"这场球对我意味着什么"缺席。这是两波升级后唯一真正"大"的剩余差距。

## 决定

1. **确定性内核扩表不换引擎**：记忆信号作为 MatchSignal 的新输入维度（FavoriteTeam / FavoritePlayer，空 = 无信号），policy 在比赛事件分支对 affect 施加有界偏置；不引入 LLM 决策。
2. **映射表 v1（锚定案例：支持的皇马丢了球 → 安慰优先于播报）**：
   - 支持的球队进球：valence +0.2，reason `policy_memory:favorite_team_goal`；
   - 支持的球员进球：valence +0.15，reason `policy_memory:favorite_player_scored`；
   - 幅度封顶 ±0.2/次，clamp 后进 [-1,1]。
3. **信号来源**：companion 在比赛事件回合从画像提取 favorite_team / favorite_player（300ms 超时、失败返回空信号）；记忆缺席 = 决策与现状逐字一致。
4. **可解释性**：每个受记忆影响的决策带 `policy_memory:<signal>` reason code 进 trace/账本——ADR-0003 的可解释优势不回退。

## 被否决的替代方案

- **失球安慰映射（对手进球 → 安慰优先）**：MatchSignal 只带进球方，无法可靠判定"支持的队是不是在场的一方"——需要赛程配对信息（等 data-provider-lite-bridge），如实留尾。
- **LLM 参与决策**：违反确定性内核纪律。
- **无界放大**：±0.2 封顶 + clamp，防止记忆把情绪推出可解释区间。

## 后果

- MatchSignal 增 Memory 字段；画像缺席 = 决策与现状一致（能力波全部 eval 零漂移的同一保证）。
- 后续映射（失球安慰、口味×话题边界联动）必须锚定新的具体案例并扩充本表，不得无案例进表。
- 信号提取复用 memory_signals.go（300ms 超时），与知识域检索共享"记忆缺席即降级"哲学。
