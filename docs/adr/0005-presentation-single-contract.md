# 0005 · 表达唯一合同：PresentationPlan，删除死代码 expression 引擎

## 背景

`backend/internal/expression/engine.go`（10 表情态、防抖、自动衰减）无任何调用方，是死代码；曾被《球球全套资料/4.产品设计/32》当作真实机制描述，而实际上线并经实战的表演路径是另一套：`relationship.PresentationPlan`（Affect/Expression/Motion/VoiceStyle 等，`backend/internal/relationship/types.go:351-360`，由 `companion/agent.go` 产出），客户端以严格白名单+别名表消费（`client/lib/services/presentation_state.dart:5-49`）。

## 决定

删除 `internal/expression` 包。Live2D 表达以 PresentationPlan 白名单为**唯一合同**：后端只发白名单内的 Affect/Expression/Motion，客户端只认白名单并做别名映射（如 `deflated→sad`）。

## 被否决的替代方案

- **接线复活 engine.go**：会引入第二套表达词汇（10 态 vs 13 表情），两套状态机并存增加防御面，且其防抖/衰减逻辑可以日后按需抄进 PresentationPlan 的演进，不需要整体复活一个平行引擎。

## 后果

- 文档与代码恢复一致：32 章迁移时围绕真实链路重写。
- 未来若需连续情绪维度（而非离散表情），在 PresentationPlan 上扩展，不重建独立引擎。
