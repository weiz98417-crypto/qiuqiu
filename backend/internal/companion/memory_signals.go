package companion

// 决策层记忆信号提取（openspec/changes/memory-in-policy，ADR-0019）：
// 从画像里取支持球队/最喜欢的球员，喂给关系策略做 affect 偏置。300ms
// 超时、失败返回空信号——记忆缺席时决策与现状逐字一致。

import (
	"context"
	"strings"
	"time"

	"qiuqiu/internal/relationship"
)

const memorySignalTimeout = 300 * time.Millisecond

// memorySignals 提取画像口味信号（favorite_team / favorite_player）。
func (a *Agent) memorySignals(ctx context.Context, userID string) relationship.MemorySignals {
	if a == nil || a.memories == nil {
		return relationship.MemorySignals{}
	}
	signalCtx, cancel := context.WithTimeout(ctx, memorySignalTimeout)
	defer cancel()
	portrait, err := a.memories.Portrait(signalCtx, userID)
	if err != nil {
		return relationship.MemorySignals{}
	}
	var signals relationship.MemorySignals
	for _, entry := range portrait.Entries {
		switch entry.SubTopic {
		case "favorite_team":
			if signals.FavoriteTeam == "" {
				signals.FavoriteTeam = strings.TrimSpace(entry.Content)
			}
		case "favorite_player":
			if signals.FavoritePlayer == "" {
				signals.FavoritePlayer = strings.TrimSpace(entry.Content)
			}
		}
	}
	return signals
}
