# 0018 · 人格设置作为粘性偏好输入

日期：2026-09-22（第二波后续，openspec/changes/settings-in-policy）

## 背景

user_character_settings（migration 048）存了三个互动规范槽位，但 policy 不读它们——设置面是摆设。policy 现有的偏好来源是每回合的 talkativeness 推导（覆盖 InitiativeMode）与 cue 词推断（AnalysisAppetite 等），都是"推断"而非"用户显式声明"。

## 决定

1. **粘性优先级**：显式设置 > cue 推断 > talkativeness 推导。设置一经存在即每回合覆盖推导值（粘性），直到被新设置替换；清空槽位即回到推导。
2. **枚举对齐**：settings 槽位值域改为与 policy 一致——initiative = natural/quiet/active（settings 侧的 normal 映射废除）；analysis_appetite = brief/detailed（policy 只有二档，standard 语义取消）。
3. **接线范围 v1**：只接 InitiativeMode 与 AnalysisAppetite 两个现成消费点（主动档位、realizer 分析深度）。banter 槽位入库不接线（policy 的调侃许可是按 scope 的 map，单值档位硬映射会发明语义）。
4. **可解释性**：覆盖发生时决策 reason codes 追加 `policy_user_setting:initiative` / `policy_user_setting:analysis_appetite`——"球球为什么这样答"始终有账可查。

## 被否决的替代方案

- **每回合设置与推导互相覆盖**：会来回翻转（用户设置 active、tier quiet，下回合又变回去）；粘性才符合"用户说了算"。
- **banter 单值档位硬映射**：scope map 结构不同，硬映射会发明语义；留待 cue 侧词汇与 scope 语义一起重整。

## 后果

- 合并点在 applyPolicy 出口（决策视图构建前），设置对 realizer prompt（分析深度）与决策视图即时生效。
- 未设置（空槽位）行为与现状逐字一致；eval 双向断言（设置生效 + 无设置不变）由 relationship 与 companion 两侧单测承担。
- banter 槽位与 cue 词入口向新状态的完全收敛如实留尾（character-settings tasks 5.2/5.3）。
