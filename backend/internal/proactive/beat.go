package proactive

import (
	"context"
	"log"
	"time"
)

// MemoryMaterializer 把被静默过期的提醒转为记忆素材（Q13）：错过的事实
// 进 recall，下一次相关聊天里自然回来。失败只记日志——素材化永不炸节拍。
type MemoryMaterializer func(ctx context.Context, reminder Reminder)

// SweepLoop 是提醒簿的服务端清理节拍。到点投递不在这里做：投递必须走
// 连接内的回合调度与投递服务（socket 归连接所有）——在线用户由其连接内
// 的低频检查投递，离线用户由连接建立时的补递投递（020 outbox 模式）。
// 本循环只负责：过期待递翻 suppressed + 转记忆素材。
func SweepLoop(ctx context.Context, store Store, materialize MemoryMaterializer, tick time.Duration) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			suppressed, err := store.SweepSuppressed(ctx, time.Now().UTC())
			if err != nil {
				log.Printf("reminder sweep error: %v", err)
				continue
			}
			for _, reminder := range suppressed {
				if materialize == nil {
					continue
				}
				materializeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				materialize(materializeCtx, reminder)
				cancel()
			}
		}
	}
}
