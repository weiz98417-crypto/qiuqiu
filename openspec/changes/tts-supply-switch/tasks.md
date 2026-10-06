# Tasks: 语音供给开关

- [x] 7.0 前置核查:tts-provider-seam 状态盘点——**已全部实施(5/5 勾)**:Synthesizer seam + Miimo adapter + instructionFor 情绪指令表 + 接线全在;本 change 零重叠,增量=本地腿 adapter + 健康探测 + 三态开关,seam 直接复用不改形状。
- [x] 7.1 本地 TTS adapter:`tts/local.go` OpenAI 兼容 `/audio/speech`(instructions 情感指令透传,音频字节直出);`SynthesizeStream(Detailed)` 以整段合成单分片实现(本地端点无标准流式,与 Miimo 回退形态一致);config QIUQIU_TTS_LOCAL_URL/MODEL/VOICE/PROBE_SECONDS;httptest 覆盖正常/非200/超时/空音频/未配置(local_test.go)。
- [x] 7.2 健康探测:`tts/local_health.go` 后台周期试合成一句短句,**连续 2 次通过才亮门**(一次通过可能是模型刚加载的窗口);快照(available/reason/probes/failures/lastCheckAt)暴露给运营台;状态翻转回调供运行中告警接线;未配置=探测整体不存在(nil)。
- [x] 7.3 三态设置:`tts/supply.go` SupplySwitch(cloud 默认透传字节级/local 态 b 失败保持并告警不回云/local_first 态 c 失败自动回云句子不断流;Detailed 路径取消不回云、分片开始后不中途换腿)+ migration 055 ops_settings(部署级单值 KV)+ `ttssupply` 设置存储(PG/Memory)+ console GET/PATCH `/api/console/tts-supply`(读=TraceRead,写=MatchWrite 幂等[Idempotency-Key,命名空间 console]+写入校验[mode 三态原值/本地选项需健康门 409]+审计 tts_supply.update+运行中即时生效)+ 运营概览「语音供给」卡(三态 Radio/健康行/回退计数/门控置灰)。evals:tts_supply_api_test.go(门控/即时生效/幂等重放/审计落账/回退端到端)+ supply_test.go(三态/回退/取消/未配置/词汇)。
- [x] 7.4 部署文档:docs/deploy/local-tts.md(CosyVoice3 docker 起法/显存要求/env 清单/切换 walkthrough/instructions 情感映射表/30 分钟验收清单)。
- [x] 7.5 门禁:go 全量 33 包绿 + console vitest 23 绿 + console 构建绿;默认态(云)字节级回归=supply_test.go TestSupplySwitchCloudDefaultPassthrough(双路径透传+零计数)+ configuredTTSSupply 不配 URL 时 local=nil 全透传。

## 双轴审查留尾(code-review 2026-10-06,均已裁定)

- **已修(review 修正 commit)**:① SupplySwitch 流式 channel 版改为 Detailed 投影——消掉本地腿 channel 路径「goroutine 内吞错」形态缺口(态 b/c 语义一处收敛);② Detailed 回退不变量显式追踪——分片交到 onChunk 手里即「已下发」,此后错误如实上抛、不换腿不回云不重投(补测试钉住);③ 态 b「保持并告警」接线——健康翻转落结构化日志(tts supply: local engine unavailable);④ GET/PATCH 501 响应形状统一、概览九格双边框消除。
- **留尾**:概览语音供给卡无轮询(健康行会陈旧,后续加 30s 轮询或 ops 推送);「云路径字节级一致」的证据是替身级 stub 双跑,真机 walkthrough(部署文档验收清单)时以真实 Miimo 补证;SupplySwitch 的非流式 Synthesize 路径无 ctx.Err() 取消特判(HTTP 层 ctx 取消自然失败,无实害的不对称);NewSupplySwitch 双腿皆 nil 时 cloudLeg nil→ErrNotConfigured(未配置部署的最后防线,未单测)。
- **tasks 10.0 核查措辞修正**(Spec 轴指出):「client/lib 零命中」欠准——moments_service.dart 是共同瞬间 API 客户端(memory-surfacing 1.4/1.5),非分享面;**「分享卡片渲染面」不存在**的结论不变。

## 前置核查与门判记录(2026-10-06,同轮波3)

- **tts-provider-seam(7.0)**:已全实施,见上。
- **knowledge-worldinfo 11.0 门判:挂起**——knowledge-retrieval 已落但条目库 21 players+15 rules=36 < 50 准入门;tasks 全空转下轮,条目录入(用户侧值班演练)过 50 后重判。
- **smart-turn-v32:挂起**——评测集素材依赖 auto-hosting soak 真实流量(用户侧待办);人工补录先行分支待用户排期。
- **teammate-journal 10.0 前置核查:分享卡片 MVP 不存在**(client/lib 零命中,演进计划的 Phase 1 项未实施)——10.4 需含最小分享渲染面;本 change 排下一轮。
- **memory-scoring**:3.4 Queue 拆分已缩编登记(RecallFusion 已是函数族),8.1-8.3 直接在函数族上改;8.4 Memobase 升级需 Docker(本机不可用,用户侧)。排下一轮。

## Sequencing

波3,独立。韵律盲测门显式挂起(等硬件)——本 change 验收不含音质,只含链路与开关正确性。与 eval-tooling 的 UTMOSv2 工具互为上下游(盲测开测时用)。
