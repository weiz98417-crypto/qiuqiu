package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"qiuqiu/internal/config"
	"qiuqiu/internal/event"
	"qiuqiu/internal/llm"
	"qiuqiu/internal/pipeline"
	"qiuqiu/internal/tts"
)

func main() {
	_ = config.Load()
	dsKey := os.Getenv("DEEPSEEK_API_KEY")
	elKey := os.Getenv("ELEVENLABS_API_KEY")

	if dsKey == "" {
		log.Fatal("DEEPSEEK_API_KEY required")
	}

	llmClient := llm.NewClient("https://api.deepseek.com/v1", dsKey)
	var ttsClient *tts.Client
	if elKey != "" {
		ttsClient = tts.NewClient(elKey)
	}

	promptMgr := pipeline.NewPromptManager()
	promptMgr.LoadSystem(readFile("prompts/v1.0/system.txt"))
	promptMgr.LoadTemplate("goal", readFile("prompts/v1.0/goal.txt"))
	promptMgr.LoadTemplate("shot", readFile("prompts/v1.0/shot.txt"))
	promptMgr.LoadTemplate("card", readFile("prompts/v1.0/card.txt"))
	promptMgr.LoadTemplate("match_status", readFile("prompts/v1.0/match_status.txt"))

	enricher := pipeline.NewContextEnricher()
	aiPipe := pipeline.NewAIPipeline(llmClient, ttsClient, promptMgr)

	ctx := context.Background()

	// Mock events: a realistic match sequence
	mockEvents := []struct {
		ev   *event.StandardEvent
		desc string
	}{
		{
			ev:   &event.StandardEvent{MatchID: 1, ID: 1, Type: "match_start", Team: "利物浦", Minute: 0, Player: event.PlayerInfo{}, Score: event.ScoreInfo{Home: 0, Away: 0}},
			desc: "比赛开始",
		},
		{
			ev:   &event.StandardEvent{MatchID: 1, ID: 2, Type: "shot", Team: "利物浦", Minute: 23, Player: event.PlayerInfo{Name: "萨拉赫"}, Score: event.ScoreInfo{Home: 0, Away: 0}},
			desc: "萨拉赫射门",
		},
		{
			ev:   &event.StandardEvent{MatchID: 1, ID: 3, Type: "yellow_card", Team: "切尔西", Minute: 35, Player: event.PlayerInfo{Name: "恩佐"}, Score: event.ScoreInfo{Home: 0, Away: 0}},
			desc: "恩佐黄牌",
		},
		{
			ev:   &event.StandardEvent{MatchID: 1, ID: 4, Type: "goal", Team: "利物浦", Minute: 67, Player: event.PlayerInfo{Name: "萨拉赫"}, Score: event.ScoreInfo{Home: 1, Away: 0}},
			desc: "萨拉赫进球!!",
		},
	}

	fmt.Println("=== Mock 全链路测试 ===")
	fmt.Println("模拟: 利物浦 vs 切尔西\n")

	totalStart := time.Now()
	for _, me := range mockEvents {
		fmt.Printf("--- [%s] %s ---\n", me.desc, me.ev.Type)

		// Run through pipeline
		enricher.UpdateState(me.ev)
		inst := &pipeline.AIGenerationInstruction{
			InstructionType: "event_reaction",
			Event:           me.ev,
			MatchContext:    enricher.Enrich(me.ev),
			Expression:      "excited",
			Priority:        me.ev.Priority(),
		}

		result := aiPipe.Process(ctx, inst)
		fmt.Printf("  LLM [%v]: %q\n", result.LLMDuration.Round(time.Millisecond), result.Text)
		if result.FallbackUsed {
			fmt.Printf("  ⚠ 降级\n")
		}
		if result.AudioData != nil {
			fmt.Printf("  TTS [%v]: %d bytes audio ✓\n", result.TTSDuration.Round(time.Millisecond), len(result.AudioData))
		}
		if result.Error != nil {
			fmt.Printf("  ❌ %v\n", result.Error)
		}
		fmt.Println()
		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("总计: %v\n", time.Since(totalStart).Round(time.Millisecond))
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
