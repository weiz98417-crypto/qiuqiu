# Tasks: Portrait Maintenance

- [ ] 5.1 migration（编号顺延）：`portrait_overlays` 加 `valid_from`/`valid_to`（nullable）；读取路径全部走时间窗查询（`WHERE (valid_to IS NULL OR valid_to > now()) AND (valid_from IS NULL OR valid_from <= now()) AND NOT deleted`）。
- [ ] 5.2 冲突操作集：`OpDecision{Op: ADD|UPDATE|DELETE|NOOP, TargetID, Reason}` 经 structured.Extract 判定（新主张 vs 同 topic 现存有效条目）；落地确定性（UPDATE=旧条目 valid_to=now+新条目；DELETE=valid_to=now 墓碑保留；NOOP=不写）。
- [ ] 5.3 Reflection 写路径接操作集 + 记忆 eval（过期偏好不出现/矛盾消解/时间窗）。
- [ ] 5.4 阶段二（5.1–5.3 验收后）：sleep-time 巩固挂 Reflection beat 尾部（近重复合并、长期未确认条目衰减=软置 valid_to）+ eval。

## Sequencing

六卡实施波 5。独立子系统，可与波 3（voice-duplex）/波 4（tts-seam）穿插推进——不同模块零冲突。5.4 前有显式验收门。
