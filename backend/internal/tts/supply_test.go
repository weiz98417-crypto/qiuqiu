package tts

import (
	"context"
	"errors"
	"testing"
)

// stubSynth 是可控的云腿替身：音频回放 + 可注入失败。
type stubSynth struct {
	audio  []byte
	fail   error
	calls  int
	stream func(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) error
}

func (s *stubSynth) Synthesize(ctx context.Context, text string, opts VoiceOpts) (*SynthesizeResult, error) {
	s.calls++
	if s.fail != nil {
		return nil, s.fail
	}
	return &SynthesizeResult{AudioData: append([]byte(nil), s.audio...), MimeType: "audio/wav"}, nil
}

func (s *stubSynth) SynthesizeStream(ctx context.Context, text string, opts VoiceOpts) (<-chan []byte, error) {
	s.calls++
	if s.fail != nil {
		return nil, s.fail
	}
	ch := make(chan []byte, 1)
	ch <- append([]byte(nil), s.audio...)
	close(ch)
	return ch, nil
}

func (s *stubSynth) SynthesizeStreamDetailed(ctx context.Context, text string, opts VoiceOpts, onChunk func(StreamChunk) error) error {
	if s.stream != nil {
		return s.stream(ctx, text, opts, onChunk)
	}
	result, err := s.Synthesize(ctx, text, opts)
	if err != nil {
		return err
	}
	return onChunk(StreamChunk{Data: result.AudioData, MimeType: result.MimeType})
}

// 默认态（cloud）双路径原样透传云腿——云 API 路径行为与开关引入前一致
// （7.5 字节级回归的锚）。
func TestSupplySwitchCloudDefaultPassthrough(t *testing.T) {
	cloud := &stubSynth{audio: []byte("cloud-audio")}
	local := NewLocalClient("")
	supply := NewSupplySwitch(cloud, local)

	if supply.Mode() != SupplyModeCloud {
		t.Fatalf("default mode = %q, want cloud", supply.Mode())
	}
	result, err := supply.Synthesize(context.Background(), "x", VoiceOpts{})
	if err != nil || string(result.AudioData) != "cloud-audio" {
		t.Fatalf("synthesize = (%q, %v)", result.AudioData, err)
	}
	if cloud.calls != 1 {
		t.Fatalf("cloud calls = %d", cloud.calls)
	}
	ch, err := supply.SynthesizeStream(context.Background(), "x", VoiceOpts{})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if audio, ok := <-ch; !ok || string(audio) != "cloud-audio" {
		t.Fatalf("stream chunk = %q", audio)
	}
	if supply.LocalFailures() != 0 || supply.CloudFallbacks() != 0 {
		t.Fatal("cloud default must not move failure counters")
	}
}

// 态 c（local_first）：本地失败自动回云（句子不断流），两个计数各自走。
func TestSupplySwitchLocalFirstFallsBackToCloud(t *testing.T) {
	cloud := &stubSynth{audio: []byte("cloud-audio")}
	local := NewLocalClient("http://127.0.0.1:1") // 不可达端点
	supply := NewSupplySwitch(cloud, local)
	supply.SetMode(SupplyModeLocalFirst)

	result, err := supply.Synthesize(context.Background(), "x", VoiceOpts{})
	if err != nil || string(result.AudioData) != "cloud-audio" {
		t.Fatalf("fallback synthesize = (%q, %v), want cloud audio", result.AudioData, err)
	}
	if supply.LocalFailures() != 1 || supply.CloudFallbacks() != 1 {
		t.Fatalf("counters = (%d, %d), want (1, 1)", supply.LocalFailures(), supply.CloudFallbacks())
	}
}

// 态 b（local）：本地失败保持并告警——不回云、错误如实上抛。
func TestSupplySwitchLocalModeKeepsAndCounts(t *testing.T) {
	cloud := &stubSynth{audio: []byte("cloud-audio")}
	local := NewLocalClient("http://127.0.0.1:1")
	supply := NewSupplySwitch(cloud, local)
	supply.SetMode(SupplyModeLocal)

	if _, err := supply.Synthesize(context.Background(), "x", VoiceOpts{}); err == nil {
		t.Fatal("local mode failure must surface, not fall back")
	}
	if cloud.calls != 0 {
		t.Fatalf("cloud calls = %d, want 0 (态 b 不回云)", cloud.calls)
	}
	if supply.LocalFailures() != 1 || supply.CloudFallbacks() != 0 {
		t.Fatalf("counters = (%d, %d)", supply.LocalFailures(), supply.CloudFallbacks())
	}
}

// 态 c 起点性失败回云；调用方取消不回云（打断语义，与 Miimo adapter 同纪律）。
func TestSupplySwitchDetailedFallbackRespectsCancel(t *testing.T) {
	cloud := &stubSynth{audio: []byte("cloud-audio")}
	local := NewLocalClient("http://127.0.0.1:1")
	supply := NewSupplySwitch(cloud, local)
	supply.SetMode(SupplyModeLocalFirst)

	var chunks []StreamChunk
	if err := supply.SynthesizeStreamDetailed(context.Background(), "x", VoiceOpts{}, func(chunk StreamChunk) error {
		chunks = append(chunks, chunk)
		return nil
	}); err != nil {
		t.Fatalf("detailed: %v", err)
	}
	if len(chunks) != 1 || string(chunks[0].Data) != "cloud-audio" {
		t.Fatalf("chunks = %+v", chunks)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	localFailuresBefore := supply.LocalFailures()
	if err := supply.SynthesizeStreamDetailed(ctx, "x", VoiceOpts{}, func(StreamChunk) error { return nil }); err == nil {
		t.Fatal("cancelled call must surface the cancellation")
	}
	if supply.LocalFailures() != localFailuresBefore {
		t.Fatal("caller cancellation must not count as a local failure")
	}
}

// 未配置本地腿时 local/local_first 态的最后防线：local 报未配置不回云;
// local_first 直接走云。
func TestSupplySwitchUnconfiguredLocalLeg(t *testing.T) {
	cloud := &stubSynth{audio: []byte("cloud-audio")}
	supply := NewSupplySwitch(cloud, nil)

	supply.SetMode(SupplyModeLocal)
	if _, err := supply.Synthesize(context.Background(), "x", VoiceOpts{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	supply.SetMode(SupplyModeLocalFirst)
	result, err := supply.Synthesize(context.Background(), "x", VoiceOpts{})
	if err != nil || string(result.AudioData) != "cloud-audio" {
		t.Fatalf("local_first without local leg = (%q, %v)", result.AudioData, err)
	}
}

// 三态词汇:归一化宽容(读取),写入校验严格。
func TestSupplyModeVocabulary(t *testing.T) {
	if NormalizeSupplyMode("  local ") != SupplyModeLocal || NormalizeSupplyMode("") != SupplyModeCloud || NormalizeSupplyMode("nonsense") != SupplyModeCloud {
		t.Fatal("NormalizeSupplyMode broken")
	}
	if !ValidSupplyMode("cloud") || !ValidSupplyMode("local") || !ValidSupplyMode("local_first") {
		t.Fatal("ValidSupplyMode must accept the three literals")
	}
	if ValidSupplyMode("LOCAL") || ValidSupplyMode("") {
		t.Fatal("ValidSupplyMode must be strict (write-path gate)")
	}
}

// 编译期锚:SupplySwitch 声明两项能力(接口实现不得静默漂移)。
var (
	_ Synthesizer          = (*SupplySwitch)(nil)
	_ StreamingSynthesizer = (*SupplySwitch)(nil)
	_ Synthesizer          = (*LocalClient)(nil)
	_ StreamingSynthesizer = (*LocalClient)(nil)
)
