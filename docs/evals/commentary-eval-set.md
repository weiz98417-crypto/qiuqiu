# 陪看解说评测集骨架（openspec/changes/eval-tooling 9.5）

> 定位:公开世界没有直播陪看 benchmark,自建是唯一路径。本档只立**骨架**
> (指标组合+样本格式),样本积累随 auto-hosting(波1B)真实流量进行——
> 样本未达门槛前本集不跑分(ADR-0023:消费先于产能)。

## 一、指标组合(抄 SoccerNet captioning,适配陪看域)

| 指标 | 用途 | 判读 |
| --- | --- | --- |
| BERTScore(F1,中文 RoBERTa 嵌入) | 生成解说 vs 参考解说的语义保真 | ≥0.85 为可用带;措辞差异不罚(球球口吻是特性不是噪声) |
| 球员实体正确率 | 提及球员是否与事件账本一致 | 硬门:错实体=坏例(与错比分同级) |
| 比分/比分演进正确率 | 提及比分是否与账本投影一致 | 硬门:零容忍(promptfoo gate 已锁对话面,本集锁批量面) |
| 事件类型命中率 | 解说所述事件类型(goal/red_card/var…)与账本一致 | ≥0.9 |
| 幻觉率 | 出现账本中不存在的事实断言 | 硬门:零容忍 |

判读纪律:**三个硬门(实体/比分/幻觉)不过=样本坏例,指标均值再高也不
过**;BERTScore 只在硬门全过后才有意义。

## 二、样本格式(每样本一行 JSONL)

```json
{
  "sampleId": "demo-2026-1006-m01-evt-003",
  "matchId": "demo-…",
  "eventId": "账本事件 id",
  "eventType": "goal",
  "eventFacts": {"team": "西班牙", "player": "佩德里", "clock": "25:00", "scoreAfter": "1-0"},
  "contextEvents": ["同回合前情事件 id 列表(≤3)"],
  "reference": "佩德里禁区前沿推射破门,西班牙先拔头筹。",
  "candidate": "球球实际输出(经 realize 全链采集,非裸 LLM)",
  "source": "operator | espn | viewer",
  "collectedAt": "2026-10-06T20:00:00+08:00"
}
```

字段纪律:`reference` 来自账本事实的人工改写(可空——可空样本只参与
硬门与实体/比分指标,不参与 BERTScore);`candidate` 必须是全链产物
(WS realize),裸 LLM 输出不进样本集——评的是球球不是底座模型。

## 三、采集与门槛

- **来源**:auto-hosting 真实比赛流量的主动话轮回复(ProactiveText 锚+
  织写产物),运营台导出;
- **门槛**:≥50 个覆盖三类事件(goal/判罚类/VAR 类)的样本才首跑;
- **跑分入口**(样本达标后建):`scripts/evals/` 下 runner,输出落
  `docs/evals/` 报告(两模型对比时同集双跑)。

## 四、状态

- [x] 指标组合+判读纪律+样本格式(本 change 9.5 交付)
- [ ] 样本积累(auto-hosting soak 流量,用户侧)
- [ ] 跑分 runner 与首份报告(样本 ≥50 后)
