package observation

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

// 气氛旁证挂点测试（ambient-audio-observation 6.3）：fake sidecar 事件 →
// AmbientCorroboration → 既有 Record 入口落库，最低权重档可见；并从
// coordinator 层锁死宪法线——旁证行永不确认/矛盾任何事实、永不产生
// 话轮文本。

func TestAmbientCorroborationCarriesLowestWeightMarkers(t *testing.T) {
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	input := AmbientCorroboration("ambient:cheer:29570000", "user-1", "match-1", "cheer", at)
	if input.Kind != KindAmbient {
		t.Fatalf("kind = %q, want %q", input.Kind, KindAmbient)
	}
	if input.EventType != "ambient_cheer" {
		t.Fatalf("eventType = %q, want ambient_cheer", input.EventType)
	}
	if input.Certainty != CertaintyAmbient {
		t.Fatalf("certainty = %q, want %q（最低权重档）", input.Certainty, CertaintyAmbient)
	}
	// 旁证在源头不携带比分/球队/球员主张——气氛变不成比分的形状闸。
	if input.ClaimedScore != nil || input.ClaimedTeam != "" || input.ClaimedPlayer != "" {
		t.Fatalf("ambient corroboration smuggled claim fields: %+v", input)
	}
	if !input.ReceivedAt.Equal(at) {
		t.Fatalf("receivedAt = %v, want %v", input.ReceivedAt, at)
	}
}

func TestAmbientEventsLandInObservationStoreAsCorroboration(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	coordinator := NewMemoryCoordinator()

	recorded, err := coordinator.Record(ctx, AmbientCorroboration("ambient:cheer:29570000", "user-1", "match-1", "cheer", at))
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if recorded.Status != StatusPendingSync {
		t.Fatalf("status = %q, want pending_sync", recorded.Status)
	}
	active, err := coordinator.ActiveObservations(ctx, "user-1", "match-1")
	if err != nil || len(active) != 1 {
		t.Fatalf("active = %+v err = %v, want one corroboration row", active, err)
	}
	if active[0].Kind != KindAmbient || active[0].Certainty != CertaintyAmbient {
		t.Fatalf("stored row = %+v, want ambient corroboration markers", active[0])
	}
	// 旁证不越出 user+match 作用域（仅观赛会话内生效）。
	if other, err := coordinator.ActiveObservations(ctx, "user-2", "match-1"); err != nil || len(other) != 0 {
		t.Fatalf("corroboration leaked across users: %+v err = %v", other, err)
	}
	// 同分钟同种类事件由既有去重合并为一个槽，不挤占活跃观察额度。
	duplicate, err := coordinator.Record(ctx, AmbientCorroboration("ambient:cheer:29570000", "user-1", "match-1", "cheer", at.Add(10*time.Second)))
	if err != nil || duplicate.ID != recorded.ID {
		t.Fatalf("duplicate ambient event created a new row: %+v err = %v", duplicate, err)
	}
}

func TestAmbientCorroborationNeverResolvesAgainstFacts(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	coordinator := NewMemoryCoordinator()
	if _, err := coordinator.Record(ctx, AmbientCorroboration("ambient:cheer:29570000", "user-1", "match-1", "cheer", at)); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// 比分事实随后确认：旁证行不得被牵动为 confirmed，也不得产生话轮文本。
	resolutions, err := coordinator.OnFactChanged(ctx, matchstate.MatchEvent{
		MatchID: "match-1", ID: "goal-1", FactID: "fact-1", FactRevision: 1,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal",
		Score:     matchstate.Score{Home: 1, Away: 0},
		CreatedAt: at.Add(5 * time.Second).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("OnFactChanged: %v", err)
	}
	for _, resolution := range resolutions {
		if resolution.ReliableText != "" {
			t.Fatalf("ambient corroboration produced follow-up text: %+v", resolution)
		}
	}
	row, ok := coordinator.Get(observationID("user-1\x00match-1\x00ambient:cheer:29570000"))
	if !ok {
		t.Fatal("ambient corroboration row disappeared")
	}
	if row.Status != StatusPendingSync {
		t.Fatalf("ambient row status = %q, want still pending_sync（气氛永不改判事实）", row.Status)
	}

	// 旁证随窗口静默过期：过期 Resolution 无 ReliableText，companion 侧过滤。
	expired, err := coordinator.Expire(ctx, row.ReconcileUntil.Add(time.Second))
	if err != nil || len(expired) != 1 {
		t.Fatalf("Expire = %+v err = %v", expired, err)
	}
	if expired[0].Status != StatusExpired || expired[0].ReliableText != "" {
		t.Fatalf("expired resolution = %+v, want silent expiry without text", expired[0])
	}
}
