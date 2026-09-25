# Design: Portrait Maintenance

## 形状

```go
type PortraitOp string // "ADD" | "UPDATE" | "DELETE" | "NOOP"

type OpDecision struct {
    Op       PortraitOp
    TargetID int64     // UPDATE/DELETE 指向现存有效条目
    Reason   string    // 判定依据，入审计
}
```

- 判定：`structured.Extract[OpDecision]`（复用既有缝，第三家生产消费者；directordraft 第一、router 第二——structured-tool-seam 3.6 迁移已随后续轮次落地，2026-09-25 校验）——输入新主张文本 + 同 topic 现存有效条目列表，输出操作。
- 落地：纯 SQL/Go，确定性——LLM 只判关系，不写库。UPDATE 写两行变更（旧 valid_to 封口 + 新条目 valid_from=now），DELETE 单行封口，墓碑行永不物理删除。

## 权威层语境（migration 041）

portrait_overlays 不只是存储：041 头注释明确它是「下一回合所见」的**权威层**——用户编辑与遗忘请求先写本地（墓碑），再 best-effort 转发 Memobase 收敛。因此冲突操作集与时间窗落在这一层，即直接作用于权威层；Memobase 转发语义维持现状不动。另：本层全部读写已过 privacy.CheckDeletion 前置（portrait_postgres.go），时间窗查询迁移必须保持该纪律。

## 时间窗语义

NULL=全域有效（存量数据迁移后全部 NULL，语义不变）；窗内交互查询只取当前有效条目；全历史查询（可解释性/调试）不过滤 valid_to——两种读法并存，接口上分开（`CurrentPortrait` vs `PortraitHistory`）。

## sleep-time 巩固（阶段二）

挂 Reflection beat 尾部顺序执行，失败不阻断 Reflection 主体（巩固是尽力而为，账本记录跳过原因）。近重复判定阈值与衰减窗口（如 90 天未确认）实施时以 eval 校准，参数进 config 不硬编码。

## 残余风险

阶段一固定槽（`preferences/user_stated`）不遮蔽 Memobase 合成槽：`favorite_team` 等合成条目由 Memobase 自行维护，固定槽里的冲突操作集封不掉它们的 `valid_to`，合成条目过期后仍可能织入措辞（eval 用例 taste-team-switch 只锁固定槽内的墓碑纪律，不覆盖该交叉路径）。收口计划归 5.4：合成槽与 `user_stated` 的对齐/失效联动（同槽互斥或合成条目失效联动），落地前措辞侧以「固定槽新值优先」缓解。

## 测试面

- 操作集：单测锁四种落地（含墓碑保留断言）；eval 用例走既有记忆 eval 形态（portrait 驱动措辞的确定性断言）。
- 判定质量：pr tier 用脚本 LLM（evals 已有 fake 路由先例）；release tier 抽样真判。
