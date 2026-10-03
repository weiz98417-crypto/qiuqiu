# Tasks: 语音供给开关

- [ ] 7.0 前置核查:openspec/changes/tts-provider-seam 状态盘点(已实施范围 vs 本 change 增量),重叠任务并入本 change 后按归档惯例处置。
- [ ] 7.1 本地 TTS adapter:OpenAI 兼容 /v1/audio/speech Synthesizer 实现 + config(端点/音色,env 名 only);httptest 覆盖正常/失败/超时。
- [ ] 7.2 健康探测:后台探测(加载+试合成)+ 连续通过门控 + 状态暴露给运营台。
- [ ] 7.3 三态设置:运营台「语音供给」(云/本地/本地优先回云)+ operator write 审计 + 运行中回退链;evals:门控/回退/审计。
- [ ] 7.4 部署文档:CosyVoice3 自托管起法(docker,含显存要求与 instruct 情感参数映射表);有卡机器切换 walkthrough。
- [ ] 7.5 门禁:go 全量 + console vitest + 默认态(云)字节级回归。

## Sequencing

波3,独立。韵律盲测门显式挂起(等硬件)——本 change 验收不含音质,只含链路与开关正确性。与 eval-tooling 的 UTMOSv2 工具互为上下游(盲测开测时用)。
