package privacy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreDeletionLifecycle(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	ctx := context.Background()

	status, err := service.Status(ctx, "usr_test")
	if err != nil || status.Status != "active" {
		t.Fatalf("initial status = %+v, err=%v", status, err)
	}
	status, err = service.RequestDeletion(ctx, "usr_test", "user_request")
	if err != nil || status.Status != "pending" || status.JobID == "" {
		t.Fatalf("pending status = %+v, err=%v", status, err)
	}
	if _, err := service.Export(ctx, "usr_test"); !errors.Is(err, ErrDeletionInProgress) {
		t.Fatalf("export during deletion error = %v", err)
	}
	if err := service.ProcessDeletion(ctx, "usr_test"); err != nil {
		t.Fatal(err)
	}
	status, err = service.Status(ctx, "usr_test")
	if err != nil || status.Status != "completed" || status.CompletedAt.IsZero() {
		t.Fatalf("completed status = %+v, err=%v", status, err)
	}
	if _, err := service.Export(ctx, "usr_test"); !errors.Is(err, ErrDataDeleted) {
		t.Fatalf("export after deletion error = %v", err)
	}
	second, err := service.RequestDeletion(ctx, "usr_test", "repeat")
	if err != nil || second.JobID != status.JobID || second.Status != "completed" {
		t.Fatalf("repeat deletion status = %+v, err=%v", second, err)
	}
}

func TestRetentionExpiryUsesConfiguredDays(t *testing.T) {
	SetRetentionDays(7)
	t.Cleanup(func() { SetRetentionDays(30) })
	now := time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC)
	if got, want := RetentionExpiry(now), now.Add(7*24*time.Hour); !got.Equal(want) {
		t.Fatalf("retention expiry = %s, want %s", got, want)
	}
}
