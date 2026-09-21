# Backchannel: 伴随反应通道（微反应不是发言）

## Why

险些进球的"哇——"必须占一个完整回合（排队/冷却/引用码门），高频微反应没通道——这恰是 Sesame/Hume 验证的"时机与节奏=陪伴感"。CONTEXT.md 早已定义 Backchannel 词条但未实现。

## What Changes

- v1 载体（F4 实证）：空 text `qiuqiu_reply` + presentation + 短文本气泡——复用客户端已有的"开场 hello"形态（只上表演不进对话流），**v1 无音频**；v1.1（客户端音频队列按 deliveryKey 配对）后置。
- 纯规则决策者（Q16）：白名单事件（big_chance/miss/save/var_check）+ 限频表 → 短语池随机（≤10 字，口语化中文，策展于 reply 词表文件）。
- 限频（Q7）：每半场 ≤3、全场 ≤6；quiet 档禁用（微反应也是打扰）；手动模式（运营正在说话/导播注入）跳过。
- 审计：不过 C2 引用码门（Q6），但每条进 trace（`backchannel.emit` ToolCall）+ Interaction Ledger；**ADR-0016**：微反应不是发言——不占回合槽、不过引用码门、受独立限频与安静档约束。

## User Stories

1. As a 球友, I want 险些进球时球球"哇"一声, so that 陪看有共振而不是只在进球后播报。
2. As a 信任守门人, I want 微反应受显式限频与审计约束, so that 通道不会退化成话痨。

## Non-goals

- SSE 流式语音（独立任务，等真消费者）；新表情资产（live2d-motion-pack 在册）；v1 音频。

## Success Criteria

- 全量 go test + eval 绿（evals 事件流不受污染——微反应独立于回合管线）；限频/白名单/quiet 档有单测锁。
