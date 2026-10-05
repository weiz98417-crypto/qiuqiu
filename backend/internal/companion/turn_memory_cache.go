package companion

// 回合经济(agent-internals 3.1):一回合内 fact 补充语织写与 realize 各拉
// 一次 recall/portrait(每次独立 Memobase HTTP+pgvector)。缓存挂在回合的
// context 上——Agent 是多连接共享的,缓存必须随请求作用域走,不能落 Agent
// 字段。同回合同 (userID, focus) 的 recall 恰一次;portrait 同回合恰一次。

import (
	"context"
	"strings"
)

type turnMemoryCacheKey struct{}

type turnMemoryCache struct {
	recall   map[string]string
	portrait map[string]string
}

func turnCache(ctx context.Context) *turnMemoryCache {
	cache, _ := ctx.Value(turnMemoryCacheKey{}).(*turnMemoryCache)
	return cache
}

// withTurnMemoryCache 给回合挂记忆缓存;无缓存 context 的调用(后台 beat/
// 评估)行为与现状逐字节一致。
func withTurnMemoryCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, turnMemoryCacheKey{}, &turnMemoryCache{
		recall:   map[string]string{},
		portrait: map[string]string{},
	})
}

// cachedRecall 返回缓存值与命中与否;miss 返回空串。
func cachedRecall(ctx context.Context, userID, focus string) (string, bool) {
	cache := turnCache(ctx)
	if cache == nil {
		return "", false
	}
	value, ok := cache.recall[userID+"\x00"+strings.TrimSpace(focus)]
	return value, ok
}

func storeRecall(ctx context.Context, userID, focus, block string) {
	if cache := turnCache(ctx); cache != nil {
		cache.recall[userID+"\x00"+strings.TrimSpace(focus)] = block
	}
}

func cachedPortrait(ctx context.Context, userID string) (string, bool) {
	cache := turnCache(ctx)
	if cache == nil {
		return "", false
	}
	value, ok := cache.portrait[userID]
	return value, ok
}

func storePortrait(ctx context.Context, userID, block string) {
	if cache := turnCache(ctx); cache != nil {
		cache.portrait[userID] = block
	}
}
