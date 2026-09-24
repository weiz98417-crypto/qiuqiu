# Design: Ambient Audio Observation

## 形状

气氛信号事件（sidecar 输出，进 observation store 的形状）：

```
{kind: "cheer" | "boo" | "volume_spike", confidence: float, session_scope: matchSessionID, ts}
```

- sidecar 接口：`POST /aevents`（音频分片 pcm16/16k，与 record 采集规格一致）→ JSON 事件数组；无状态、不落盘、会话外不接收。
- 旁路挂点：服务端转发 ASR 分片处并行旁送（不阻塞 ASR 主路，旁路失败静默丢弃+计数）。

## ADR-0002 相容性论证

观察通道（observation store）本就收纳外部主张并做确认/矛盾/悬置；气氛事件只作**旁证**（corroborating evidence，最低权重档），不构成 Fact Claim 主体、不产生 Match Fact——数据源与运营台仍是事实源的全部。此规则写进本 change 的 eval 负例（见 proposal Success Criteria），未来任何「气氛直接改判」的提议都会撞上这条测试。

## 部署与摘除

- compose 服务 `sensevoice-aed`（CPU 可跑；1660 GPU 可选加速，不依赖）；后端经内网 HTTP 调用，超时短（如 500ms）+ 熔断，失败静默。
- pr tier 用 fake sidecar（脚本内置确定性事件），真 sidecar 只在 nightly/release 冒烟。

## 隐私口径（写入设置面文案）

「观看比赛时，球球会分析现场声音的气氛（欢呼/嘘声）来陪你看球；音频不会被保存，比赛结束后不留任何声音记录。」
