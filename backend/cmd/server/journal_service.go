package main

// 球友手记生成服务（openspec/changes/teammate-journal 10.1）：赛后 beat
// 的消费端——从账本终场投影取事实（MatchFacts 快照）、从画像取口味素材，
// 交给 journal.Generate（LLM 织写→比分校验→失败降级确定性底稿）。生成是
// 尽力而为：任何素材/LLM/落库失败都只记日志，绝不影响 reflection 主体。

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"qiuqiu/internal/journal"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/memory"
)

// journalService 是手记域在 server 装配里的聚合：生成（afterPostMatch 挂
// reflection beat）与 API（journalStore/journalMoments 直供 handleJournalAPI）。
type journalService struct {
	store     journal.Store
	moments   journal.MomentSource
	generator journal.TextGenerator
	matches   matchstate.Repository
	memories  *memory.Queue
}

func newJournalService(matches matchstate.Repository, memories *memory.Queue, store journal.Store, generator journal.TextGenerator) *journalService {
	return &journalService{
		store:     store,
		moments:   newQueueMomentSource(memories),
		generator: generator,
		matches:   matches,
		memories:  memories,
	}
}

// queueMomentSource 把 memory.Queue 的 Moment 投影适配成装订源（按场过滤
// 取内容）——journal 包不反向依赖 memory 包。
type queueMomentSource struct {
	memories *memory.Queue
}

func newQueueMomentSource(memories *memory.Queue) *queueMomentSource {
	return &queueMomentSource{memories: memories}
}

// MomentsForMatch 返回该场的共同瞬间内容（Moment 投影按 matchID 过滤）。
func (s *queueMomentSource) MomentsForMatch(ctx context.Context, userID, matchID string) []string {
	if s == nil || s.memories == nil {
		return nil
	}
	moments, err := s.memories.ListMoments(ctx, userID, 100, 0)
	if err != nil {
		return nil
	}
	out := make([]string, 0)
	for _, moment := range moments {
		if moment.MatchID == matchID && strings.TrimSpace(moment.Content) != "" {
			out = append(out, moment.Content)
		}
	}
	return out
}

// afterPostMatch 是 post_match 分支的手记挂点（生成尽力而为，失败只记日志）。
func (s *journalService) afterPostMatch(ctx context.Context, userID, matchID string) {
	if s == nil || s.store == nil || strings.TrimSpace(matchID) == "" {
		return
	}
	journalCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	facts, portrait := s.material(journalCtx, userID, matchID)
	if facts == nil {
		return
	}
	if _, err := journal.Generate(journalCtx, s.store, s.generator, userID, *facts, portrait); err != nil {
		log.Printf("journal: generate for user %q match %q: %v", userID, matchID, err)
	}
}

// material 从账本终场投影与画像取生成素材。账本缺该场事实（未托管/已清）
// 返回 nil——手记只写真实看过的比赛。
func (s *journalService) material(ctx context.Context, userID, matchID string) (*journal.MatchFacts, []journal.PortraitLine) {
	snapshot := s.matches.PublicSnapshot(matchID)
	if strings.TrimSpace(snapshot.HomeTeam) == "" || strings.TrimSpace(snapshot.AwayTeam) == "" {
		return nil, nil
	}
	if snapshot.Score.Home == 0 && snapshot.Score.Away == 0 && len(snapshot.KeyEvents) == 0 {
		// 0-0 且无事件：无素材支撑（防把空账本写成「0-0 闷平」）。
		return nil, nil
	}
	facts := &journal.MatchFacts{
		MatchID:  matchID,
		HomeTeam: snapshot.HomeTeam,
		AwayTeam: snapshot.AwayTeam,
		Score:    fmt.Sprintf("%d-%d", snapshot.Score.Home, snapshot.Score.Away),
		Season:   s.seasonLabel(matchID),
	}
	for _, event := range snapshot.KeyEvents {
		if event.EventType != "goal" {
			continue
		}
		goal := strings.TrimSpace(event.PlayerName)
		if goal == "" {
			continue
		}
		if clock := strings.TrimSpace(event.Clock); clock != "" {
			goal += " " + clock
		}
		facts.Goals = append(facts.Goals, goal)
	}
	portrait := make([]journal.PortraitLine, 0, 2)
	if s.memories != nil {
		portraitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		entries, err := s.memories.Portrait(portraitCtx, userID)
		if err == nil {
			for _, entry := range entries.Entries {
				line := journal.PortraitLine{SubTopic: entry.SubTopic, Content: entry.Content}
				if strings.TrimSpace(line.Content) != "" {
					portrait = append(portrait, line)
				}
				if len(portrait) >= 3 {
					break
				}
			}
		}
	}
	return facts, portrait
}

// journalSeasonLabel 从比赛配置推导赛季标签（kickoff 年份；无 kickoff 用
// 当前年）——赛季册的装订键。
func (s *journalService) seasonLabel(matchID string) string {
	if s != nil && s.matches != nil {
		if config := s.matches.Config(matchID); strings.TrimSpace(config.Kickoff) != "" {
			if parsed, err := time.Parse(time.RFC3339, config.Kickoff); err == nil {
				return fmt.Sprintf("%d", parsed.Year())
			}
		}
	}
	return fmt.Sprintf("%d", time.Now().Year())
}
