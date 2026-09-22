# Design: Knowledge Event Triggers

## 触发管线（搭 ActReact 便车，不立独立话轮）

```
判罚事件 → Library.TriggerLookup(eventType) → 命中?
  → 命中：条目(answer+quote) 进 realizer 语境（hard 指令：引号原样携带 quote）
      → 生成后 contains(reply, quote)?
          是 → 正常投递，trace knowledge:<id>，计数+1
          否 → 降级：reply 尾部追加「补一句规则：{answer}」（verbatim），计数+1
  → 未命中：现状路径，零改动
```

前置门（先于触发查询）：限频（同条目每场1/总量2）、quiet 档、用户正在说话让路（与 backchannel 同判据）。ADR-0015 的引用码机器不启用——附句不是独立 Proactive Turn。

## 限频状态

连接级（与 backchannel.State 同生命周期）：`map[entryID]struct{}` + 总计数器，每场重置（period 变更清空）。

## YAML 形状

```yaml
id: rule-var-check
triggers: ["var_check", "var_overturn"]
quote: "手球判罚要看是否故意张开手臂扩大防守面积"
answer: "……（完整规则陈述，逐字锚定 source）……"
topics: [...]
source: "IFAB Laws of the Game, Law 12"
confidence: 0.95
effective_at: 2026-09-23
```

quote 是 answer 的可引用子句或其口语化浓缩——但**必须逐字出现在最终回复里**，语义由 contains 守卫保证。策展纪律：quote ≤40 字、无夸张语气词（规则陈述不带感情）。

## eval 断言形态

确定性：contains(reply, quote) 或（降级路径）contains(reply, answer)。织写语气归 G 的 judge 离线档。
