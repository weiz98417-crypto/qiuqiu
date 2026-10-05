package memory

// 引用序列持久化的重启存活测试(agent-internals 3.5):写入→新 store 实例
// (模拟重启)仍能取走全部序列——审计链跨重启不断的回归锚。DATABASE_URL
// 门控,与 threads/overlay integration 同纪律。

import (
	"context"
	"os"
	"testing"

	"qiuqiu/internal/matchstate"
)

func TestPostgresCitationStoreSurvivesRestart(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	migrated, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	defer migrated.Close()

	userID := "citation-restart-" + os.Getenv("CI_TEST_RUN")
	// 预清理:重跑幂等。
	seed, err := OpenRecords(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open records (cleanup): %v", err)
	}
	if _, err := seed.TakeCitations(ctx, userID); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	seed.Close()

	// 实例一:写入三条引用序列后关闭(模拟进程退出)。
	first, err := OpenRecords(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open records (first): %v", err)
	}
	for _, sequence := range []int64{11, 22, 33} {
		if err := first.AppendCitation(ctx, userID, sequence); err != nil {
			t.Fatalf("append %d: %v", sequence, err)
		}
	}
	// 幂等:同序列重复写不翻倍。
	if err := first.AppendCitation(ctx, userID, 22); err != nil {
		t.Fatalf("append duplicate: %v", err)
	}
	first.Close()

	// 实例二(模拟重启):消费即删,一次取回全部且有序。
	second, err := OpenRecords(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open records (second): %v", err)
	}
	defer second.Close()
	sequences, err := second.TakeCitations(ctx, userID)
	if err != nil {
		t.Fatalf("take citations: %v", err)
	}
	if len(sequences) != 3 || sequences[0] != 33 || sequences[1] != 22 || sequences[2] != 11 {
		t.Fatalf("sequences = %v, want [33 22 11] (newest first)", sequences)
	}
	// 消费即删:再次取为空。
	if again, err := second.TakeCitations(ctx, userID); err != nil || len(again) != 0 {
		t.Fatalf("second take = %v/%v, want empty (consume-once)", again, err)
	}
}
