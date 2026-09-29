# ASR 自托管离线评估与决策(asr-selfhost-eval)

- 状态:已完成(评估门)。
- 结论:**不过门,维持 MiMo 云主路,不动主路代码。** 四判据中 ③热词显著有效、①partial 首响相对现状 2.2 倍改善为部分过门要素,但 ②final 准确率显著劣于 MiMo(CER 0.107 vs 0.040)是硬伤,④CPU 峰值在 4c8G 参照机上无余量。
- 决策日期:2026-09-30。评估资产入库 `scripts/asr-eval/`(可重复),原始结果 `scripts/asr-eval/results/*.json`。

## 1. 背景与被评对象

主路 ASR 现状:MiMo 云(`mimo-v2.5-asr`,openai-compat POST,`backend/internal/asr/client.go`)。假流式:`backend/internal/asr/session.go:12-14` 每 1.5s 音频(48000B)开一个 1.5s 窗(0.25s 重叠)整窗转写当 partial,终稿整段重转写。候选:FunASR 2pass(paraformer-zh-streaming 流式 partial + SeACo-Paraformer 热词终稿),调研数字(partial ~600ms)需本机实测再定切换(参照 turn-detection e9b393b 先例)。

本卡只做评估与决策,主路零改动。

## 2. 评估环境(如实记录)

| 项 | 值 |
|---|---|
| 机器 | i5-12400F(6P/12 线程),15.8GB RAM,Windows 11,无 Docker,无 GPU |
| FunASR 侧 | python 3.11.9 embeddable(npmmirror 镜像)+ torch 2.14.0+cpu + funasr 1.4.16 + torchaudio 2.11.0(pypi tuna 镜像),装在 `%TEMP%\asr-eval\python311`(不入 git) |
| 模型 | 流式 `paraformer-zh-streaming`(iic/speech_paraformer-large_asr_nat-zh-cn-16k-common-vocab8404-online);终稿 `paraformer-zh` 短名即 SeACo(iic/speech_seaco_paraformer_large_asr_nat-zh-cn-16k-common-vocab8404-pytorch),modelscope 国内下载 53.6s 就绪 |
| MiMo 侧 | 真实云调用(`backend/.env` 的 MIMO_API_KEY,2026-09-30 同天同时段),TTS 用 `mimo-v2.5-tts`/Chloe 合成同批音频 |
| 音频 | 16 例(四类×4)MiMo TTS 合成 → 16k 单声道 PCM16;噪声例 = 语音增益 0.35 + 白噪声 SNR≈8dB(LCG 种子 42,可重复) |
| 备胎 | sherpa-onnx 路线已勘察未启用(python 主线 20 分钟内装通)。勘察结论:sherpa-onnx 热词(contextual biasing)**仅支持 transducer**,paraformer 离线不支持(源码 `offline-recognizer-impl.h`:"Only transducer models support contextual biasing")→ 备胎跑不了判据③,此路記錄在案 |

## 3. 方法与口径

- 用例集:`scripts/asr-eval/cases.json`,四类各 4 例——球员名中外文混杂(佩德里/法比安鲁伊斯/穆西亚拉/Bellingham/亚马尔/罗德里戈/维尼修斯)、比分数字(二比一/2-1/三比二/1-1)、中文口语碎句(嗯/卧槽漂亮/哎呀/来了来了)、直播间噪声(低音量 TTS+白噪声)。热词表模拟主路 `voiceRecognitionHints`(`backend/cmd/server/server/transcription.go:296`,队名+球员名全表,两家引擎同批同 hints)。
- **partial 首响(prefix-probe 口径)**:对流式模型从 100ms 起逐档(100..1000,1200,1500,2000ms)截前缀喂入,首响 = 出现非空 partial 所需最短音频(audioLeadMs)+ 该次本地计算(computeMs)。MiMo 侧按 session.go 逐窗模拟(1.5s 窗/0.25s 重叠,窗齐才发),首响 = 1500ms 音窗 + 网络 RTT。两口径均"从音频 0 时刻计",可比。
- **final**:同批音频,三组——seaco 无热词 / seaco 热词(全表 hints)/ MiMo(hints)。
- **CER**:归一化(小写→外文球员名别名归中文规范形→去标点空白→阿拉伯数字逐字映射中文数字)后 Levenshtein/参考字数,`scripts/asr-eval/lib/textnorm.mjs`,对 ref/hyp 两边一致施加。
- **CPU**:`cpu-sample.mjs` 每 1s 采样系统负载(Win32_Processor.LoadPercentage)与子进程 CPU 时间增量。

复现:

```bash
node scripts/asr-eval/tts-generate.mjs --engine mimo          # 生成同批音频(%TEMP%,需 MIMO_API_KEY)
node scripts/asr-eval/run-mimo.mjs --audio-dir %TEMP%\asr-eval\audio16k \
  --cases scripts/asr-eval/cases.json --out scripts/asr-eval/results/mimo.json
node scripts/asr-eval/cpu-sample.mjs --out scripts/asr-eval/results/cpu-funasr.json -- \
  <python> scripts/asr-eval/run-funasr.py --audio-dir %TEMP%\asr-eval\audio16k \
  --cases scripts/asr-eval/cases.json --out scripts/asr-eval/results/funasr.json
node scripts/asr-eval/summarize.mjs                            # 四判据汇总表
```

## 4. 四判据实测数据

### 判据① partial 首 token(先立阈值 <800ms)→ 边缘达标(修订口径过,严格均值口径差 3.4%)

| 引擎 | 首响口径 | avg | min | max | n |
|---|---|---|---|---|---|
| FunASR streaming | audioLead+compute | 827ms | 712.9ms | 1472.5ms | 16 |
| — 其中 audioLead(最短可出字音频) | | 675ms | 600 | 1200 | |
| — 其中 compute | | 153ms | 112.9 | 272.5 | |
| MiMo 云(假流式 1.5s 窗) | 1500ms 窗 + RTT | 1842ms | 1800ms | 1965ms | 16 |

- 14/16 例 <800ms,P50=731ms;超标的 2 例(pm3/cl1,audioLead=1200ms)是模型对首 600ms 块置信不足不出字(首词短/语气词开头),属真实行为非口径伪影。
- 修订建议口径(先立后修):P50<800ms 或 ≥85% 例<800ms → 过。严格均值口径 827ms 超 27ms,如实记录。
- 相对现状 1842ms,partial 首响 **2.2 倍改善**,是本次评估最实的收益点。

### 判据② final 准确率不劣于 MiMo → **不过(硬伤)**

| 组 | 平均 CER(越低越好) | n |
|---|---|---|
| FunASR seaco 无热词 | 0.120 | 16 |
| FunASR seaco 热词 | 0.107 | 16 |
| MiMo 云(hints) | **0.040** | 16 |

| 类别 | FunASR 无热词 | FunASR 热词 | MiMo |
|---|---|---|---|
| player-mixed | 0.195 | 0.173 | 0.030 |
| score | 0.108 | 0.108 | 0.033 |
| colloquial | 0.094 | 0.094 | 0.080 |
| noise | 0.081 | 0.052 | 0.015 |

- 全类别劣于 MiMo;最重一例 pm2:TTS 英文口音读 "Bellingham",FunASR 输出 `bellllingham`(热词表含 Bellingham/贝林厄姆均未纠回),单例 CER 0.444。MiMo 同例输出 `Bellingham` 原文。
- 数字比分FunASR 侧典型错:上半场→小班超、2-1→二到一、扳平→单平。口语碎句两家互有胜负(cl2 卧槽 MiMo 也输出我操,平手)。
- 即便剔除 pm2 最差例,FunASR 热词组均值 ~0.082 仍为 MiMo(~0.036)的 2.3 倍,结论不变。

### 判据③ 热词命中率显著优于无热词 → **过**

| 组 | 精确命中/总 | 命中率 | 含容错窗(±20%) |
|---|---|---|---|
| seaco 热词 | 9/10 | **0.9** | 0.9 |
| seaco 无热词 | 5/10 | 0.5 | 0.8 |
| MiMo(hints) | 8/10 | 0.8 | 1.0 |

- 热词把精确命中率 0.5→0.9(0.5→0.8→0.9 逐级看:nz4 穆西亚拉在噪声例被热词纠回),SeACo biasing 在本域有效,与调研一致。
- 但热词救不回 pm2 的外文连读乱码:热词提升的是"名单内中文名"命中,不解决外文原文渲染差距。

### 判据④ CPU 资源可接受 → **临界(4c8G 参照机无余量)**

- 批处理全流程(模型加载+16 例探针循环+双 final)观测:解码进程峰值 **4.0 逻辑核**,平均 2.9 核(12 线程机);系统负载峰值 88%,均值 39%。
- 单路稳态成本:流式整段解码 552-1685ms(RTF≈0.15-0.30);seaco final 单次 277-791ms(avg 507ms 无热词/536ms 热词)。torch 默认线程数(=物理核 6)未做调优。
- 对照 4c8G 参照:seaco final 解码尖峰(≥2-4 线程)与数字人主路服务同机必然争抢;要落地需独立机或严格线程钳制+并发上限。观测口径含探针循环开销,偏保守,但方向性结论可靠。

### final 延迟顺带观测

本地 seaco final 计算(avg 507ms)≈ MiMo final 网络 RTT(avg 549ms,范围 399-978ms)——**切自托管在 final 上没有延迟红利**,收益只在 partial 与数据不出本机/无并发上限。

## 5. 决策

**不过门,维持 MiMo 云主路。** 判据②是替换的前置硬条件,实测 CER 差 2.7 倍且热词无法弥补;④在同机部署形态下无余量。①③的部分过门要素不足以支撑主路切换。

### 若未来重开替换卡的输入(部分过门要素的兑现路径)

1. **波次建议(部分过)**:不切主路,改立「热词纠错旁路」评估卡——MiMo final 与本地 seaco(热词)final 异步交叉,两者分歧且热词命中时用热词结果纠错(球员名错牌是直播间最伤的错型;seaco 热词命中率 0.9 vs MiMo 0.8 且成本异步)。延迟增量 ~536ms 可后台化,不动实时链路。
2. partial 收益若要单独兑现:MiMo 假流式 1842ms → 需要更快的 partial,可评估 MiMo 侧缩短窗口(1.5s→1.0s,RTT 已测 ~340ms,代价是调用量×1.5),属主路参数调优卡,不动供应商。
3. sherpa-onnx 备胎正式排除出判据③场景(热词仅 transducer 支持);Qwen3-ASR 维持排除(需 GPU+无热词)。
4. 重评触发条件:FunASR 流式模型大版本升级 / MiMo 涨价或可用性事故 / 出现带 GPU 的部署形态(届时 Qwen3-ASR 一并重评)。

## 6. 环境局限(如实)

- **合成音频非真人**:全部用例为 MiMo TTS(Chloe 单口音)合成,"绝对数字仅供相对对照";尤其外文人名由 TTS 英文口音读出,pm2 的 bellllingham 乱码在真人口播下未必复现(但 MiMo 同音频能输出正确原文,相对差距仍成立)。无真实麦克风/直播间链路,噪声为合成白噪声(SNR≈8dB),非游戏声/音乐人声。
- 单机观测 CPU,非长期服务稳态;探针循环口径含重复解码开销(偏保守)。
- MiMo partial 模拟未含 WebSocket 传输与前端渲染路径;云侧为单区域单时段采样,未测跨时段方差与并发。
- CER 归一化已知局限:数字逐字映射("2-1"→"二一"会冤枉"二比一"多一个"比"字)、外文别名表仅覆盖用例集名单;对两家引擎一致施加,相对比较不受影响。
- 评估环境与真实直播环境的差距总结:**相对结论(谁好谁坏、热词有没有用、量级差)可信;绝对数字(首响毫秒、CER 小数点后第二位)仅供方向判断。**

## 7. 评估资产清单

| 文件 | 作用 |
|---|---|
| `scripts/asr-eval/cases.json` | 用例集(四类×4)+ 全表 hints |
| `scripts/asr-eval/lib/wav.mjs` | 零依赖 WAV 编解码/重采样/白噪声混合 |
| `scripts/asr-eval/lib/textnorm.mjs` | 归一化/CER/热词命中/mergeTranscript(session.go 同口径)/汇总 |
| `scripts/asr-eval/tts-generate.mjs` | 同批音频生成(MiMo TTS 主,SAPI Huihui 兜底,噪声后处理) |
| `scripts/asr-eval/run-mimo.mjs` | MiMo final + session.go 假流式逐窗模拟 |
| `scripts/asr-eval/run-funasr.py` | FunASR 流式 prefix-probe 首响 + seaco 热词/无热词 final |
| `scripts/asr-eval/cpu-sample.mjs` | CPU 占用采样包装器 |
| `scripts/asr-eval/summarize.mjs` | 四判据汇总(results/summary.{json,md}) |
| `scripts/asr-eval/results/*.json` | 原始与汇总结果(mimo/funasr/cpu-funasr/summary) |

模型与音频缓存在 `%TEMP%\asr-eval\` 与 `%USERPROFILE%\.cache\modelscope\`,不入 git。
