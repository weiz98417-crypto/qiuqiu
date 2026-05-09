package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"qiuqiu/internal/config"
	"qiuqiu/internal/datasource"
)

func main() {
	_ = config.Load()

	apiKey := os.Getenv("APISPORTS_API_KEY")
	if apiKey == "" {
		log.Fatal("APISPORTS_API_KEY not set")
	}

	client := datasource.NewClient(apiKey)

	fmt.Println("=== 今日比赛 ===")
	fixtures, err := client.GetTodayFixtures()
	if err != nil {
		log.Printf("获取今日比赛失败: %v", err)
	} else if len(fixtures) == 0 {
		fmt.Println("今日暂无比赛")
	} else {
		for _, f := range fixtures {
			fmt.Printf("  [%d] %s vs %s | %s | %d'\n",
				f.ID, f.HomeTeam, f.AwayTeam, f.Status, f.Elapsed)
		}
	}

	fmt.Println("\n=== 实时比赛 ===")
	live, err := client.GetLiveFixtures()
	if err != nil {
		log.Printf("获取实时比赛失败: %v", err)
		return
	}
	if len(live) == 0 {
		fmt.Println("当前无实时比赛")
	}

	for _, f := range live {
		fmt.Printf("\n[%d] %s %d-%d %s | %d'\n",
			f.ID, f.HomeTeam, f.HomeGoal, f.AwayGoal, f.AwayTeam, f.Elapsed)

		events, err := client.GetEvents(f.ID)
		if err != nil {
			log.Printf("  获取事件失败: %v", err)
			continue
		}
		if len(events) == 0 {
			fmt.Println("  (暂无事件)")
		}
		for _, e := range events {
			fmt.Printf("  %d' %s - %s: %s\n", e.Time.Elapsed, e.Type, e.Player.Name, e.Detail)
		}
		if len(events) > 0 {
			b, _ := json.MarshalIndent(events[:min(3, len(events))], "", "  ")
			fmt.Printf("\n  原始事件格式:\n%s\n", string(b))
		}
	}
}
