# 本地 TTS 引擎部署(Fun-CosyVoice3)与语音供给切换

> openspec/changes/tts-supply-switch 7.4。云 API(MiMo)继续现役;本文档
> 面向「有一张 ≥6GB 显存显卡的机器」——从零到运营台可切换,目标 30 分钟。

## 一、起本地引擎(docker)

推荐镜像形态:任何暴露 **OpenAI 兼容 `/v1/audio/speech`** 端点的
CosyVoice3 服务都可以;以下以 Fun-CosyVoice3-0.5B(Apache-2.0,instruct
情感指令,流式首包 ~150ms)为例:

```bash
docker run -d --gpus all --name cosyvoice-tts \
  -p 9880:8000 \
  -e MODEL=Fun-CosyVoice3-0.5B \
  breakstring/cosyvoice-api-server:latest
# 以实际使用的镜像为准;唯一硬要求:
#   POST /v1/audio/speech  {model, input, voice?, response_format?, instructions?}
#   → 200, body = 音频字节(推荐 response_format=wav)
```

显存:0.5B 模型 4-6GB 可推理;CPU 推理可行但首句延迟秒级,不建议陪看
现役。IndexTTS-2(表现力天花板)商用授权+≥12GB,记录为备选不实施。

## 二、后端接线(env,密钥只记变量名)

```bash
# 本地腿端点(留空=本地腿整体不存在,云 API 独役——默认形态)
QIUQIU_TTS_LOCAL_URL=http://127.0.0.1:9880/v1
# 模型/音色(可留空走 adapter 默认 cosyvoice-v3)
QIUQIU_TTS_LOCAL_MODEL=Fun-CosyVoice3-0.5B
QIUQIU_TTS_LOCAL_VOICE=
# 健康探测周期秒数(默认 30)
QIUQIU_TTS_LOCAL_PROBE_SECONDS=30
```

重启后端后:健康探测循环开始试合成(连续 2 次通过才亮门);运营台
「运营概览 → 语音供给」卡的本地选项由置灰变为可选。

## 三、切换 walkthrough(运营台)

1. 打开运营台 → 运营概览 → **语音供给** 卡;
2. 看健康行:「本地引擎健康」= 可切换;置灰显示原因 = 先修引擎;
3. 选三态之一,**应用切换**(即时生效,无需重启;写走幂等+审计):
   - **云 API(现役)**:默认态,与引入本地腿前行为一致;
   - **本地优先 · 失败回云**:推荐切换态——本地腿故障自动回云,句子
     不断流(回云兜底计数在卡上可见);
   - **本地引擎**:完全本地;运行中本地失败**不**回云(你显式选择了
     本地),失败计数累加并在卡上可见。
4. 切换记录进运营审计(tts_supply.update);重启后保持上次选择
   (ops_settings 表)。

## 四、情感指令映射表

后端把情绪状态翻成自然语言表演指令(`tts.InstructionFor`,ADR-0012
修订),经 OpenAI 兼容载荷的 `instructions` 字段下发。CosyVoice3 的
instruct 通道吃自然语言,常用映射:

| 场景 | 指令示例(服务端已生成,此处供调引擎参数) |
| --- | --- |
| 进球兴奋解说 | 「用极度兴奋激动的语气解说,语速稍快」 |
| 丢球惋惜 | 「用失落惋惜的语气,语速放慢,声音低沉」 |
| VAR 紧张 | 「用紧张而专注的语气,压低声音」 |
| 闲聊平静 | 「用轻松自然的语气闲聊」 |

引擎侧不支持 `instructions` 字段时该字段被忽略(表现力降级,功能不坏)。

## 五、验收清单(30 分钟 walkthrough 的判据)

- [ ] `curl -s http://127.0.0.1:9880/v1/audio/speech ...` 返回音频;
- [ ] 后端日志出现 `user affect`…无关;应看到探测成功(无 `local tts error`);
- [ ] 运营台语音供给卡:本地选项亮起,应用「本地优先」后合成走本地;
- [ ] 杀掉 docker 容器 → 卡上健康转红 + 原因;态「本地优先」下客户端
      语音不断流(回云);态「本地引擎」下如实失败;
- [ ] 云 API 默认态回归:不配 `QIUQIU_TTS_LOCAL_URL` 时一切如旧。
