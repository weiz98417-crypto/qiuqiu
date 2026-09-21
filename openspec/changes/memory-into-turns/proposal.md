# Memory Into Turns: 记忆进入主动回合、回访与事实补充语

## Why

Recall/Portrait 的注入面只有闲聊 realization 一个窄口（realizeReply 一处调用）：进球后的主动反应念运营罐头、开线程回访是罐头拼装、事实应答看不到记忆——而 CONTEXT.md 明文要求画像「wired into what Qiuqiu actually says」。数字球友的产品承诺是关系连续性，最需要记忆的回合（进球反应、承诺兑现回访）恰恰是记忆缺席的回合。

## What Changes

- 新增 `internal/companion/memory_realization.go`：`realizeWithMemory` 带 recall 重措辞 + `appendFactMemoryCallback` 事实应答补充语 + 回访合成决策载体。
- 主动回合（handleMatchEvent）：运营 ProactiveText 是**锚点**（grilling Q3）——仅 recall 材料非空且 director 给出 Speech 决策时带记忆重措辞，guard 不过回原文；recall 检索词取事件实体（球员优先、球队兜底，contains 语义）。
- 开线程回访（RecoverOpenThreads）：回访文本同样仅记忆材料非空时重措辞，trace 的 emit 模式如实标记 realized/deterministic。
- 事实应答补充语：fact 意图应答后追加一句记忆衔接（一句为限、单独过 guard、锚源放宽到记忆材料），失败/拒绝整句丢弃，事实本体措辞不动（design decision 4）。
- 独立任务 A（零 Go 代码）：Memobase `enable_event_embedding` 打开 + bge-m3 端点配置（SiliconFlow，key 未到位留空，降级=现状 contains）。

## User Stories

1. As a 老球友, I want 进球的是我喜欢的球员时球球带记忆反应, so that 陪伴感来自「他记得我」而不只是播报。
2. As a 用户, I want 之前没答完的问题被回访时带着上下文, so that 补答不像罐头。
3. As a 延迟守门人, I want 无记忆材料时一切原文出去, so that 快路径与 evals 零漂移。

## Non-goals

- 决策层不动：记忆不参与关系 Director 的沟通动作选择（CAS 策略表与 director_test 原样）；Portrait/Recall 仅作为措辞上下文与（留给 proactive-scheduler 的）引用码候选来源。
- pgvector 第二 adapter 独立任务（做不出如实留尾，不阻塞本 change）。
- relationship/memory 双记忆词汇统一 stretch 后置。

## Success Criteria

- 新增单测锁保守门三态（材料非空+guard 过=重措辞；无材料=原文；guard 拒=原文）。
- 100 eval case 全绿（evals 不接记忆种子，全部走原文路径）；全量 go test 绿。
