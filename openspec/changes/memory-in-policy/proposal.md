# Memory In Policy: 决策层的记忆信号

## Why

能力波 #2 只开了措辞层与"引用码候选来源"一个口：Portrait/Recall 影响球球**怎么说**，不影响**选什么沟通动作**。后果是"支持的球队丢了球，球球该先安慰你，却在一视同仁地播报"——决策层看不见"你支持皇马"（画像里有，策略输入没有）。这是两波升级后唯一真正"大"的留尾。

## What Changes

- 「记忆信号 × 决策输出」映射表：哪个记忆维度影响哪个决策输出、幅度、可解释 reason code——**实施前经一轮小 grilling 定稿**（锚定案例：支持球队丢球 → 安慰优先于播报），并落 ADR。
- Director.Apply 增记忆信号输入维度（确定性策略表扩展，不引入 LLM 决策）：信号可含画像口味（支持球队/球员）、Recall 强相关时刻；幅度保守，先窄后宽。
- 每个新映射带 reason code 进 trace/账本（决策可解释性不回退）。
- director_test 扩展 + 新行为 eval（scripted memory fixture，离线）。

## User Stories

1. As a 皇马球迷, I want 皇马丢球时球球先安慰我, so that 决策层知道"这场球对我意味着什么"。
2. As a 可解释性守门人, I want 每个受记忆影响的决策留 reason code, so that ADR-0003 的可解释优势不因记忆而回退。

## Non-goals

- 不引入 LLM 决策、不重建策略表（扩表不换引擎）。
- 不做无案例驱动的映射——每个进表的映射必须锚定一个具体产品案例。
- C2 引用码门、Guard、话题边界纪律原样。

## Success Criteria

- 全量 go test + eval 绿；新增映射各有 director_test 断言 + 行为 eval。
- 决策可解释：受记忆影响的决策在 trace/账本带记忆 reason code。

## Sequencing

settings-in-policy 之后（同族由浅入深：先设置后记忆）；实施前置 grilling 覆盖映射表与 ADR。
