// supply.go 承载语音供给三态开关（tts-supply-switch 7.3）：云 API（默认/
// 现役）、本地引擎、本地优先·失败回云。供给是部署级事实非用户偏好——
// 全局单值，运行中即时生效（atomic 快照），切换由运营台写入（写入校验
// 本地腿健康门控与持久化在 cmd/server 的 settings 面完成，本包只管
// 「当前态是什么」与「一次合成走哪条腿」）。
//
// 默认态（cloud）下每条路径都原样透传云腿——云 API 路径行为与开关引入
// 前逐字节一致（7.5 回归的锚）。三态语义：
//   - cloud：永远云腿；
//   - local：永远本地腿；运行中失败不回云（用户显式选择了本地），错误
//     如实上抛（「保持并告警」，告警计数见 Fallback 计数面）；
//   - local_first：本地失败自动回云（「句子不断流」）。
package tts

import (
	"context"
	"strings"
	"sync/atomic"
)

// SupplyMode 是三态词汇（运营台/存储/日志共用）。
const (
	SupplyModeCloud      = "cloud"
	SupplyModeLocal      = "local"
	SupplyModeLocalFirst = "local_first"
)

// NormalizeSupplyMode 把任意输入归一到三态；未知/空值落默认 cloud。
func NormalizeSupplyMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case SupplyModeLocal:
		return SupplyModeLocal
	case SupplyModeLocalFirst:
		return SupplyModeLocalFirst
	default:
		return SupplyModeCloud
	}
}

// ValidSupplyMode 报告字符串是否是三态原值（写入校验用——归一化是读取
// 宽容，写入要严格）。
func ValidSupplyMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case SupplyModeCloud, SupplyModeLocal, SupplyModeLocalFirst:
		return true
	}
	return false
}

// SupplySwitch 是三态分发的 Synthesizer：cloud 腿必在；local 腿可为 nil
// （未配置）——local/local_first 态在未配置时如实报 ErrNotConfigured
// （运营台写入校验应先拦住，这是运行中的最后防线）。
type SupplySwitch struct {
	cloud Synthesizer
	local *LocalClient

	mode atomic.Value // string，三态原值
	// localFailures 计「local/local_first 态下本地腿失败」的累计：态 b
	// 的「保持并告警」与态 c 的回云都计数（运营观测面）。
	localFailures atomic.Int64
	// cloudFallbacks 计态 c「本地失败回云成功兜底」的累计。
	cloudFallbacks atomic.Int64
}

func NewSupplySwitch(cloud Synthesizer, local *LocalClient) *SupplySwitch {
	if cloud == nil {
		cloud = local // 全云未配置的部署（开发环境）里本地腿也可以独自服役
	}
	return &SupplySwitch{cloud: cloud, local: local}
}

// cloudLeg 返回云腿；双腿皆未配置时 nil（调用方报未配置）。
func (s *SupplySwitch) cloudLeg() Synthesizer {
	if s == nil {
		return nil
	}
	if s.cloud == nil && s.local != nil {
		return s.local
	}
	return s.cloud
}

// Mode 返回当前三态。
func (s *SupplySwitch) Mode() string {
	if s == nil {
		return SupplyModeCloud
	}
	if mode, ok := s.mode.Load().(string); ok {
		return NormalizeSupplyMode(mode)
	}
	return SupplyModeCloud
}

// SetMode 运行中切换（即时生效）。
func (s *SupplySwitch) SetMode(mode string) {
	if s == nil {
		return
	}
	s.mode.Store(NormalizeSupplyMode(mode))
}

// LocalFailures 返回本地腿失败累计（三态 b「保持并告警」的观测面）。
func (s *SupplySwitch) LocalFailures() int64 {
	if s == nil {
		return 0
	}
	return s.localFailures.Load()
}

// CloudFallbacks 返回态 c 回云兜底累计。
func (s *SupplySwitch) CloudFallbacks() int64 {
	if s == nil {
		return 0
	}
	return s.cloudFallbacks.Load()
}

func (s *SupplySwitch) Synthesize(ctx context.Context, text string, opts VoiceOpts) (*SynthesizeResult, error) {
	if s == nil || s.cloudLeg() == nil {
		return nil, ErrNotConfigured
	}
	switch s.Mode() {
	case SupplyModeLocal:
		result, err := s.synthesizeLocal(ctx, text, opts)
		if err != nil {
			return nil, err // 态 b：保持并告警（计数），不回云
		}
		return result, nil
	case SupplyModeLocalFirst:
		result, err := s.synthesizeLocal(ctx, text, opts)
		if err == nil {
			return result, nil
		}
		// 态 c：本地失败自动回云，句子不断流。
		s.cloudFallbacks.Add(1)
		return s.cloudLeg().Synthesize(ctx, text, opts)
	default:
		return s.cloudLeg().Synthesize(ctx, text, opts)
	}
}

// SynthesizeStream 是 Synthesizer 接口的流式位：channel 版由 Detailed
// 投影生成——三态回退/计数语义只在 Detailed 一处收敛，本地腿 channel
// 路径「goroutine 内吞错」的形态缺口不再暴露给调用方（LocalClient 的
// channel 版保留仅为满足接口，运行中失败=空 channel，无错误通道）。
func (s *SupplySwitch) SynthesizeStream(ctx context.Context, text string, opts VoiceOpts) (<-chan []byte, error) {
	if s == nil || s.cloudLeg() == nil {
		return nil, ErrNotConfigured
	}
	ch := make(chan []byte, 4)
	go func() {
		defer close(ch)
		_ = s.SynthesizeStreamDetailed(ctx, text, opts, func(chunk StreamChunk) error {
			select {
			case ch <- chunk.Data:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	return ch, nil
}

// SynthesizeStreamDetailed 实现 StreamingSynthesizer：按态转发。态 c 的
// 回退只发生在「本地分片尚未开始回调」的失败上——一旦开始下发，不中途
// 换腿（换源音频拼接会重复/断裂），onChunk 报错或本地腿已回调后的错误
// 如实上抛、不记失败不回云（回退不变量显式追踪，不靠 adapter 惯例）。
func (s *SupplySwitch) SynthesizeStreamDetailed(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) error {
	if s == nil || s.cloudLeg() == nil {
		return ErrNotConfigured
	}
	delivered := false
	gated := func(chunk StreamChunk) error {
		// 「已下发」= 分片交到 onChunk 手里——哪怕它随即报错中止，也不可
		// 回云重投（换源拼接会重复内容），错误如实上抛。
		if len(chunk.Data) > 0 {
			delivered = true
		}
		return onChunk(chunk)
	}
	switch s.Mode() {
	case SupplyModeLocal:
		if err := s.local.SynthesizeStreamDetailed(ctx, text, opts, gated); err != nil {
			if !delivered && ctx.Err() == nil {
				s.localFailures.Add(1)
			}
			return err
		}
		return nil
	case SupplyModeLocalFirst:
		if s.local.Configured() {
			err := s.local.SynthesizeStreamDetailed(ctx, text, opts, gated)
			if err == nil {
				return nil
			}
			if delivered || ctx.Err() != nil {
				return err // 分片已下发或调用方取消：不换腿、不回云
			}
			s.localFailures.Add(1)
			s.cloudFallbacks.Add(1)
		}
		return s.streamCloudDetailed(ctx, text, opts, onChunk)
	default:
		return s.streamCloudDetailed(ctx, text, opts, onChunk)
	}
}

func (s *SupplySwitch) streamCloudDetailed(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) error {
	if streaming, ok := s.cloudLeg().(StreamingSynthesizer); ok {
		return streaming.SynthesizeStreamDetailed(ctx, text, opts, onChunk)
	}
	// 云腿无流式能力（测试替身等）：整段合成后单分片回调。
	result, err := s.cloudLeg().Synthesize(ctx, text, opts)
	if err != nil {
		return err
	}
	if onChunk == nil {
		return nil
	}
	return onChunk(StreamChunk{Data: result.AudioData, MimeType: result.MimeType})
}

// synthesizeLocal 是 local 腿的失败记账收口（态 a 不会走到 local）。
func (s *SupplySwitch) synthesizeLocal(ctx context.Context, text string, opts VoiceOpts) (*SynthesizeResult, error) {
	result, err := s.local.Synthesize(ctx, text, opts)
	if err != nil {
		s.localFailures.Add(1)
		return nil, err
	}
	return result, nil
}
