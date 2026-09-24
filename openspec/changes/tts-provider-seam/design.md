# Design: TTS Provider Seam

## 形状

```go
type Synthesizer interface {
    Synthesize(ctx context.Context, text string, opts VoiceOpts) ([]byte, error)
    // SynthesizeStream 为流式位预留：签名进 interface、Miimo adapter 返回 ErrNotSupported，
    // 真 stream adapter 由 transport 决策后的供应商切换提供。不实现假流式。
}
type VoiceOpts struct {
    Instruction string // 自然语言风格指令，经 Miimo instruction 通道下发
    Format      string
    Voice       string
}
```

- 错误面：供应商错误与熔断 open 原样上抛（韧性归调用方，ADR-0012 纪律不变）；fake 不失败（除注入测试）。
- Miimo adapter 即现 client.go 逻辑搬迁：URL/model/voice 默认值、WAV 组包、base64 解码、熔断器全部进 adapter；`SynthesizeWithInstruction` 的 instruction-as-user-message 拼装进 adapter，调用方只见 VoiceOpts.Instruction。

## 情绪→指令映射（deep module）

- 纯函数 `instructionFor(affect AffectState, act CommunicationAct, utterLen int) string`：表驱动——情绪档（激动/平静/遗憾/调侃/紧张…）× 沟通动作（react/opine/recall/backchannel/repair…）→ instruction 模板；utterLen 参与（Backchannel 短反应→「短促、口语、一闪而过」类紧凑指令）。
- 确定性：无 LLM、无随机——快照测试锁全表。
- AffectState 来源：既有情绪状态（matchstate/事件驱动的 affect 计算），不新增状态源。

## adapter 清单

Miimo（生产，现役迁移）/ fake（测试，静音 WAV）。真实云 adapter 后置，触发条件见 tasks Sequencing。

## 依赖

零新依赖（结构化判定不需要；映射是纯函数）。ADR-0012 修订注随 4.1 入库。
