# Tasks: Memory Into Turns

- [x] 2.1 memory_realization.go：realizeWithMemory（focus 参数）+ factCallbackDecision + appendFactMemoryCallback + threadRecoveryDecision。
- [x] 2.2 handleMatchEvent 集成（锚点措辞、实体 focus、Reason=proactive_memory_realized）。
- [x] 2.3 RecoverOpenThreads 集成（trace 构建顺序调整、emit 模式如实标记）。
- [x] 2.4 HandleBoundaryRequest 事实补充语接线。
- [x] 2.5 memory_realization_test.go：保守门三态 + 补充语两态，5 个单测。
- [x] 2.6 独立任务 A：Memobase enable_event_embedding + bge-m3 端点配置（deploy/memobase/config.yaml + docker-compose env）。
- [x] 2.7 验证：全量 go test 绿 + 100 eval 绿。
- [ ] 2.8 独立任务 B（可留尾）：pgvector 第二 adapter（写入+召回合并+contains 降级）——若本波未完成，如实记录移交。

## Sequencing

能力波第 2 个，依赖 intent-registry（isFactIntent 已是注册表查表）。proactive-scheduler 的引用码第三钥匙（画像口味）依赖本 change 把 Portrait/Recall 带进主动回合管线。
