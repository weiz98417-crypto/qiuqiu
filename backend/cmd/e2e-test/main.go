package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"qiuqiu/internal/config"
	"qiuqiu/internal/datasource"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/pipeline"
	"qiuqiu/internal/tts"
)

func main() {
	_ = config.Load()

	apiKey := os.Getenv("APISPORTS_API_KEY")
	dsKey := os.Getenv("DEEPSEEK_API_KEY")
	elKey := os.Getenv("ELEVENLABS_API_KEY")

	if apiKey == "" || dsKey == "" {
		log.Fatal("APISPORTS_API_KEY and DEEPSEEK_API_KEY required")
	}

	client := datasource.NewClient(apiKey)
	llmClient := llm.NewClient("https://api.deepseek.com/v1", dsKey, os.Getenv("DEEPSEEK_MODEL"))
	var ttsClient *tts.Client
	if elKey != "" {
		ttsClient = tts.NewClient(elKey)
	}

	// Find live matches
	live, err := client.GetLiveFixtures()
	if err != nil || len(live) == 0 {
		log.Fatal("no live matches")
	}

	f := live[0]
	fmt.Printf("=== 全链路测试 ===\n")
	fmt.Printf("比赛: %s %d-%d %s [%d']\n\n", f.HomeTeam, f.HomeGoal, f.AwayGoal, f.AwayTeam, f.Elapsed)

	// Build pipeline
	promptMgr := pipeline.NewPromptManager()
	promptMgr.LoadSystem(readFile("prompts/v1.0/system.txt"))
	promptMgr.LoadTemplate("goal", readFile("prompts/v1.0/goal.txt"))
	promptMgr.LoadTemplate("shot", readFile("prompts/v1.0/shot.txt"))
	promptMgr.LoadTemplate("card", readFile("prompts/v1.0/card.txt"))
	promptMgr.LoadTemplate("match_status", readFile("prompts/v1.0/match_status.txt"))

	engine := pipeline.NewEngine(nil, nil, pipeline.NewContextEnricher())
	aiPipe := pipeline.NewAIPipeline(llmClient, ttsClient, promptMgr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)

	// Start polling
	eventChan := engine.EventChan()
	poller := datasource.NewPoller(client, int64(f.ID), eventChan)
	poller.SetScore(f.HomeGoal, f.AwayGoal)
	go poller.Run(ctx)

	// Process output
	fmt.Println("等待事件...")
	for inst := range engine.Output() {
		fmt.Printf("\n--- 事件: %s (P%d) ---\n", inst.Event.Type, inst.Priority)
		fmt.Printf("  球员: %s | 分钟: %d | 比分: %s\n",
			inst.Event.Player.Name, inst.Event.Minute, inst.MatchContext.ScoreAfter)

		result := aiPipe.Process(ctx, inst)
		fmt.Printf("  LLM: %q (%v)\n", result.Text, result.LLMDuration.Round(time.Millisecond))
		if result.FallbackUsed {
			fmt.Printf("  ⚠ 降级模式\n")
		}
		if result.AudioData != nil {
			fmt.Printf("  TTS: %d bytes (%v)\n", len(result.AudioData), result.TTSDuration.Round(time.Millisecond))
		}
		if result.Error != nil {
			fmt.Printf("  ❌ 错误: %v\n", result.Error)
		}
	}
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
