package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"qiuqiu/internal/config"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/tts"
)

type LatencyRecord struct {
	Index       int
	Prompt      string
	LLMDuration time.Duration
	TTSDuration time.Duration
	Total       time.Duration
	Text        string
	Err         error
}

func main() {
	cfg := config.Load()

	if cfg.DeepseekAPIKey == "" {
		log.Fatal("DEEPSEEK_API_KEY not set")
	}
	if cfg.ElevenLabsKey == "" {
		log.Println("⚠ ELEVENLABS_API_KEY not set — skipping TTS")
	}

	llmClient := llm.NewClient(cfg.DeepseekBaseURL, cfg.DeepseekAPIKey)
	var ttsClient *tts.Client
	if cfg.ElevenLabsKey != "" {
		ttsClient = tts.NewClient(cfg.ElevenLabsKey)
	}

	var records []LatencyRecord
	ctx := context.Background()
	testEvents := llm.TestEvents

	fmt.Printf("=== LLM + TTS 延迟测试 (%d 次调用) ===\n\n", len(testEvents))

	for i, prompt := range testEvents {
		fmt.Printf("[%d/%d] ", i+1, len(testEvents))
		rec := LatencyRecord{Index: i, Prompt: prompt}

		// LLM call
		llmStart := time.Now()
		result, err := llmClient.Generate(ctx, prompt)
		if err != nil {
			rec.Err = fmt.Errorf("llm: %w", err)
			fmt.Printf("LLM FAIL: %v\n", err)
		} else {
			rec.LLMDuration = result.Duration
			rec.Text = result.Text
			fmt.Printf("LLM=%v text=%q ", rec.LLMDuration.Round(time.Millisecond), rec.Text)

			// TTS call
			if ttsClient != nil {
				ttsResult, err := ttsClient.Synthesize(ctx, rec.Text, "cgSgspJ2msm6clMCkdW9") // Bella — premade voice, free tier OK
				if err != nil {
					rec.Err = fmt.Errorf("tts: %w", err)
					fmt.Printf("TTS FAIL: %v", err)
				} else {
					rec.TTSDuration = ttsResult.Duration
					rec.Total = time.Since(llmStart)
					fmt.Printf("TTS=%v total=%v (%d audio bytes)",
						rec.TTSDuration.Round(time.Millisecond),
						rec.Total.Round(time.Millisecond),
						len(ttsResult.AudioData))
				}
			} else {
				rec.Total = rec.LLMDuration
			}
		}
		fmt.Println()

		records = append(records, rec)
		time.Sleep(200 * time.Millisecond) // avoid rate limiting
	}

	// Report
	fmt.Println("\n=== 延迟报告 ===")

	var llmDurations, ttsDurations, totalDurations []time.Duration
	success := 0
	for _, r := range records {
		if r.Err == nil {
			llmDurations = append(llmDurations, r.LLMDuration)
			ttsDurations = append(ttsDurations, r.TTSDuration)
			totalDurations = append(totalDurations, r.Total)
			success++
		}
	}

	if len(llmDurations) > 0 {
		sort.Slice(llmDurations, func(i, j int) bool { return llmDurations[i] < llmDurations[j] })
		sort.Slice(ttsDurations, func(i, j int) bool { return ttsDurations[i] < ttsDurations[j] })
		sort.Slice(totalDurations, func(i, j int) bool { return totalDurations[i] < totalDurations[j] })

		fmt.Printf("成功: %d/%d\n", success, len(records))
		fmt.Printf("LLM 延迟:  P50=%v  P95=%v  min=%v  max=%v\n",
			p50(llmDurations), p95(llmDurations), llmDurations[0], llmDurations[len(llmDurations)-1])
		fmt.Printf("TTS 延迟:  P50=%v  P95=%v  min=%v  max=%v\n",
			p50(ttsDurations), p95(ttsDurations), ttsDurations[0], ttsDurations[len(ttsDurations)-1])
		fmt.Printf("端到端:    P50=%v  P95=%v  min=%v  max=%v\n",
			p50(totalDurations), p95(totalDurations), totalDurations[0], totalDurations[len(totalDurations)-1])

		// Gate check
		p95Total := p95(totalDurations)
		if p95Total < 3*time.Second {
			fmt.Printf("\n✅ 闸门通过: P95 端到端延迟 %v < 3s\n", p95Total.Round(time.Millisecond))
		} else {
			fmt.Printf("\n❌ 闸门未通过: P95 端到端延迟 %v > 3s\n", p95Total.Round(time.Millisecond))
			fmt.Println("   建议回退到方案A（纯文字+语音，推迟 Live2D）")
		}
	} else {
		fmt.Println("所有调用均失败，请检查 API Key 和网络连接")
	}
}

func p50(d []time.Duration) time.Duration {
	return d[len(d)*50/100]
}

func p95(d []time.Duration) time.Duration {
	return d[len(d)*95/100]
}
