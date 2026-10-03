# 评估工具:UTMOSv2 盲测先筛 + promptfoo CI 门禁

## Why

两件评估基建(调研 C5,消费者明确):

1. **UTMOSv2 韵律盲测先筛**:韵律盲测第二次开测(TTS 换型/本地腿有卡后)的人工成本需要减半器——UTMOSv2(MIT,VoiceMOS 2024 Track1 七项第一)自动 MOS 预测 1-5 分,候选先粗筛人耳只评头部。**域差注记**:对中文高表现力 TTS 只粗筛不下结论(ADR-0023:人耳结论必须真实采集)。
2. **promptfoo 高阶用法**:promptfoo 已有观测层(promptfoo-observability),缺三件——CI 门禁(eval 失败阻断 merge)/对话级用例(整段陪看含主动话轮,断言「没抢话/没说错比分」)/HTTP custom provider(直评 Go 服务,免导出 prompt)。另:陪看解说自建评测集的指标组合抄 SoccerNet captioning(BERTScore+球员/比分实体正确率)——公开世界没有直播陪看 benchmark,自建是理由。

## What Changes

- **UTMOSv2 工具链**:scripts/ 下 Python 工具(独立 venv,不进 Go 依赖)——批量预测 MOS+排序输出;与 tts-supply-switch 的盲测门衔接(盲测开测时人耳只评 ≥阈值头部)。
- **promptfoo CI 门禁**:GitHub Action 跑 eval 断言失败即红(pr tier 之上加 LLM 行为层)。
- **对话级用例**:陪看整段对话(含主动话轮)进 promptfoo,llm-rubric/factuality 断言;红队模块做「诱导球球报错比分」对抗集(宪法负例,与 Go evals 的 safety 套件互补——那边锁确定性,这边锁措辞)。
- **HTTP provider**:promptfoo 直评本地起的 Go 服务(startEvalBackend 同形环境)。
- **陪看解说评测集骨架**:指标组合(BERTScore+实体正确率)与样本格式定义——样本积累依赖 auto-hosting 真实流量,本 change 只立骨架。

## Non-goals

- UTMOSv2 进 CI(盲测是事件性不是每次提交);人耳终审替代(粗筛 only)。
- LLM-as-judge 模型选型研究(用现役 mimo 自评,效果不行再说)。
- 解说评测集样本生产(骨架 only,样本随 auto-hosting 流量积累)。

## Success Criteria

- UTMOSv2 工具对 10 段样本出排序,与后续人耳盲测排序相关性记录;
- CI 门禁:故意注错一轮 eval→Action 红;对话级用例含至少一条抢话负例与一条错比分负例;
- HTTP provider 跑通本地 Go 服务全链。
