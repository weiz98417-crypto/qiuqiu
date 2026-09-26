# Tasks: Portrait Maintenance

- [x] 5.1 migration（编号顺延）：`portrait_overlays` 加 `valid_from`/`valid_to`（nullable）；读取路径全部走时间窗查询（`WHERE (valid_to IS NULL OR valid_to > now()) AND (valid_from IS NULL OR valid_from <= now()) AND NOT deleted`）。
- [x] 5.2 冲突操作集：`OpDecision{Op: ADD|UPDATE|DELETE|NOOP, TargetID, Reason}` 经 structured.Extract 判定（新主张 vs 同 topic 现存有效条目）；落地确定性（UPDATE=旧条目 valid_to=now+新条目；DELETE=valid_to=now 墓碑保留；NOOP=不写）。
- [x] 5.3 Reflection 写路径接操作集 + 记忆 eval（过期偏好不出现/矛盾消解/时间窗）。
- [x] 5.4 阶段二（5.1–5.3 验收后）：sleep-time 巩固挂 Reflection beat 尾部（decayPortraitEntries：按年龄衰减，PORTRAIT_DECAY_DAYS 默认 90 可关；grilling 定案——「确认」无逐条信号面，改按年龄口径，重复主张经 UPDATE 刷新 ValidFrom 天然续期）+ eval（taste-team-decay：AdvanceDays 推进时钟 + runner 代跑生产同一 DecayBefore，eval 不跑 queue beat）。**合成槽收口**（残余风险解法）：OpDecision.ShadowSlots 经判定器 schema 输出（候选 PORTRAIT_SHADOW_SLOTS 默认 basic_info/favorite_team、basic_info/favorite_player），UPDATE/DELETE 落地后对指认合成槽写开放墓碑——ResolvePortrait 每次读取遮蔽，不怕 Memobase 重提取；无判定器的盲路径保守不写 shadow。DecayBefore 集成测试（make_interval SQL）待部署轮 pgvector 真库。

## Sequencing

六卡实施波 5。独立子系统，可与波 3（voice-duplex）/波 4（tts-seam）穿插推进——不同模块零冲突。5.4 前有显式验收门。
