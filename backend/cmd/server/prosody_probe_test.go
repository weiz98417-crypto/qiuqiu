package main

// 韵律盲测探针(verification & consumption 轮,voice-streaming-delivery 留尾):
// 同一段回复合成「整段版」与「逐句流式版」两套音频,交给用户人耳盲听——
// 判据:逐句版 >30% 差评才返工句间指令,否则维持现状。两版都走生产函数
// (整段=SynthesizeResponse;逐句=SynthesizeResponseStream,含逐句 instruction
// 与 WAV24k 组帧;合并=collectingResponseSink.Audio() 真实逻辑)。
//
// 运行(需 MIMO_API_KEY 与网络):
//   QIUQIU_PROSODY_OUT=../artifacts/prosody-blind go test ./cmd/server -run TestProsodyProbeBlindPairs -v -timeout 15m

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/conversation"
	"qiuqiu/internal/relationship"
	"qiuqiu/internal/tts"
)

var prosodyTexts = []string{
	"好球！佩德里这脚推射角度太刁了，门将一点办法都没有。",
	"先别急，VAR 在看有没有越位。我总觉得这球悬。",
	"你说的那个换人我记下了，下次他上场我第一时间告诉你。",
	"这场裁判的尺度是有点松。放在欧冠，这球早吃牌了。",
	"赢下这场就能保住前四，我比你还紧张。",
	"说实话，穆西亚拉今天状态一般，好几个球都没接住。",
	"点半场了，去倒杯水吧，下半场我喊你。",
	"这要是进了就是绝杀，我的心跳都快跟不上了。",
	"别气别气，输一场而已，下周国家德比再找回场子。",
	"我跟你说，这个教练的换人时机一直是这个风格，慢半拍。",
}

func neutralTalkPlan() relationship.PresentationPlan {
	return relationship.PresentationPlan{Expression: "chat", Motion: "speak"}
}

func TestProsodyProbeBlindPairs(t *testing.T) {
	outDir := os.Getenv("QIUQIU_PROSODY_OUT")
	apiKey := os.Getenv("MIMO_API_KEY")
	if outDir == "" {
		t.Skip("QIUQIU_PROSODY_OUT 未设置:盲测探针按需运行")
	}
	if strings.TrimSpace(apiKey) == "" {
		t.Fatal("MIMO_API_KEY 未设置:盲测需要真实 MiMo 合成")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	client := tts.NewClient(apiKey)
	adapter := responseSpeechSynthesizer{synthesizer: client}
	acts := []relationship.CommunicationAct{}
	ctx := context.Background()

	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	keyLines := []string{"# 韵律盲测答案键(勿在盲听前查看)", ""}

	for index, text := range prosodyTexts {
		whole, err := adapter.SynthesizeResponse(ctx, text, neutralTalkPlan(), acts)
		if err != nil {
			t.Fatalf("pair %d whole synthesis: %v", index+1, err)
		}
		// 逐句版:生产 SynthesizeResponseStream 产帧,经 collecting sink 合并。
		sink := &collectingResponseSink{}
		streamErr := adapter.SynthesizeResponseStream(ctx, text, neutralTalkPlan(), acts, func(sentence conversation.StreamedSentence) error {
			return sink.DeliverAudio(ctx, conversation.AudioDelivery{
				Data: sentence.Data, MIME: sentence.MIME,
				SentenceIndex: sentence.SentenceIndex, SentenceCount: sentence.SentenceCount,
			})
		})
		if streamErr != nil {
			t.Fatalf("pair %d streamed synthesis: %v", index+1, streamErr)
		}
		streamed := sink.Audio()

		wholeVariant := "a"
		streamedVariant := "b"
		if random.Intn(2) == 1 {
			wholeVariant, streamedVariant = "b", "a"
		}
		variants := map[string][]byte{
			wholeVariant:    whole.Data,
			streamedVariant: streamed.Data,
		}
		for variant, data := range variants {
			name := filepath.Join(outDir, fmt.Sprintf("pair-%02d-%s.wav", index+1, variant))
			if err := os.WriteFile(name, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		keyLines = append(keyLines, fmt.Sprintf(
			"- pair-%02d: A=%s(%s) B=%s(%s)",
			index+1, wholeVariant, "整段", streamedVariant, "逐句",
		))
	}

	sort.Strings(keyLines)
	key := strings.Join(keyLines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(outDir, "answer-key.md"), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d pairs to %s", len(prosodyTexts), outDir)
}
