# 唤醒词 KWS:前台空闲态「嘿球球」

## Why

非比赛时段/静默档,用户要开口得先点按;「喊一声就聊」是陪伴产品的自然形态。sherpa_onnx pub 包(Apache-2.0,全平台)KeywordSpotter + 中文 zipformer 现成模型(sherpa-onnx-kws-zipformer-wenetspeech-3.3M),keywords.txt 拼音改词免训练,客户端本地推理无云端依赖。**误触是主约束**:「球球」双音节在足球语境高频(用户聊球说到「球球」即误触),首发必须三音节以上。openWakeWord/microWakeWord 中文路已死,不选。

## What Changes

- **范围=前台空闲态**:app 打开、无活跃语音会话(非比赛时段/静默档)时喊「嘿球球」直接开一轮对话。后台常驻(Android foreground service 常听)是产品升级,**后置单独拍板**。
- **wake_service(独立服务)**:唤醒态与语音会话态互斥切换——空闲态 KWS 监听(VAD 不跑,省电)→ 唤醒 → 起会话 duplex(照现状)→ 会话结束回 KWS 监听;不往 match_session_controller(1,584 行)里堆状态。
- **词表配置化**:首发「嘿球球」(hēi qiú qiú,三音节);keywords.txt 拼音改词免训练,换词不改代码。
- **误触评估门(先于投产)**:足球解说+真实对话样本集;判据=足球语境误触 <1 次/小时、安静环境唤醒率 >95%;不过门→换词表重测,仍不过→不投产。
- **唤醒后行为**:开一轮听(复用既有 asr_start 会话流程);presentation events 表加 `wake` 条目(ADR-0007 单一源,球球抬头看你);无后续语音超时回静默(复用静默兜底)。
- **隐私**:本地推理、音频不落盘;麦克风监听**默认关**,设置页显式开启+首启一句话引导(opt-in 纪律)。
- **平台**:Android 先行;web 排除(页面失焦麦克风即停);iOS 出范围留配置位。
- 模型 wenetspeech-3.3M **int8**;参数起步照 xiaozhi 实践(keywords_score=1.8 / threshold=0.1 / cooldown=1.5s),实测调。

## User Stories

1. As a 用户, I want 非比赛时段喊一声球球就能聊, so that 陪伴感不断线。
2. As a 用户, I want 麦克风监听我自己开关, so that 隐私自己掌控。

## Non-goals

- 后台/息屏常驻(Android foreground service;产品决策后置单独拍板)。
- iOS(出范围,留配置位)。
- web 形态(失焦即停,无意义)。
- 比赛会话中的唤醒词(duplex 已常开,冗余)。

## Success Criteria

- 误触评估门判据通过(评估资产入库,像 turn-detection 15 用例那样沉淀);
- 空闲态唤醒→会话 e2e;超时回静默;设置页开关生效(默认关);
- wake_service 状态机 flutter test 用例;
- 前台监听功耗不显著;
- web 构建不受影响(web 排除但构建必须不坏——9b6e1f0 教训)。
