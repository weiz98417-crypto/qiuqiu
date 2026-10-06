# Tasks: 知识域 World Info 参数学

- [x] 11.0 门判:**门开（2026-10-07 重判通过）**——knowledge-retrieval 已落 ✓ 且条目库门槛上调 **100 条**(anysearch 调研锚点:elfsight「约 20 主题×100 FAQ 上限」/itwrites 500 企业基准「20-30 核心起步分层扩容」/IFAB Laws 17 章骨架;200 登记二期扩容目标不设门——避免逼出凑数条目)。**已扩至 121 条**(rules 100+players 21:规则细化 17 章×3+术语 30+赛制 12+常识 7)且全量解析导入实测通过(gate 121≥100)（原 36 条:21 players+15 rules;本轮起草 16 条真实高频观赛知识[角球/界外球/任意球/球门球/补时/加时/点球大战/门线技术/门将规则/累积停赛/有利原则/帽子戏法/高位逼抢/零封/反击/金靴金球/世界波/开球]入库,顺手清理存量死文件 rule-offside.yaml/rule-penalty.yaml——与 judgment-* 变体同 id 的重复文件,PutIfAbsent 幂等早已掩盖）。**常驻门卫**:gate_test.go（TestKnowledgeDirCountsOverGate,对 KNOWLEDGE_DIR 根生产同路径递归导入,跌破 50 即红——删条目悄悄破门会当场暴露）。knowledge-retrieval 落地 ✓、条目库 52 ≥ 50 ✓——**两门全过,本 change 解挂,11.1-11.3 可实施**。
- [ ] 11.1 schema 扩展:priority/inclusionGroup/stickyTurns/cooldownTurns/probability(默认值=现状行为)+ migration。
- [ ] 11.2 检索后处理管道:预算裁剪→互斥消歧→生命周期→概率(全确定性);evals:四机制各一族+默认值字节级一致。
- [ ] 11.3 策展台表单扩展 + 测试。

## Sequencing

波3 尾,门内再评估(见 11.0)。
