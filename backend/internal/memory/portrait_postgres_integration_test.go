package memory

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/privacy"
)

// TestPostgresPortraitOverlayTemporalIntegration locks migration 051's
// temporal ledger on the real SQL path: window reads (NULL=全域), the four op
// landings, tombstone preservation (永不物理删), the privacy 前置纪律 and the
// one deliberate physical-delete exception. Skipped without DATABASE_URL,
// like every Postgres integration test.
func TestPostgresPortraitOverlayTemporalIntegration(t *testing.T) {
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
	records, err := OpenRecords(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenRecords: %v", err)
	}
	defer records.Close()

	userID := "pg-portrait-user-" + time.Now().UTC().Format("150405")
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = records.pool.Exec(cleanupCtx, `DELETE FROM privacy_tombstones WHERE user_id = $1`, userID)
		_, _ = records.pool.Exec(cleanupCtx, `DELETE FROM portrait_overlays WHERE user_id = $1`, userID)
	}
	defer cleanup()

	// 存量语义（NULL=全域）：Put 出来的行双窗皆空，任何时刻都取得到。
	added, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpAdd}, PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我最支持的球队是巴萨。"})
	if err != nil {
		t.Fatalf("ApplyPortraitOp ADD: %v", err)
	}
	if added.ID == 0 || added.ValidFrom.IsZero() || !added.ValidTo.IsZero() {
		t.Fatalf("ADD row = %+v, want ledger id, valid_from stamped, valid_to open", added)
	}
	// 同槽开放行检查（migration 052）：槽内已有开放行时 ADD 显式报
	// ErrSlotOccupied——误判在落地前拦下，不等索引冲突裸上抛。
	if _, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpAdd}, PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我现在最支持的球队是皇马。"}); !errors.Is(err, ErrSlotOccupied) {
		t.Fatalf("ADD into an occupied slot = %v, want ErrSlotOccupied", err)
	}

	// UPDATE：旧条目封口 + 新条目生效，封口行留在库里可回放。
	upserted, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpUpdate, TargetID: added.ID}, PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我现在最支持的球队是皇马。"})
	if err != nil {
		t.Fatalf("ApplyPortraitOp UPDATE: %v", err)
	}
	current, err := records.CurrentPortrait(ctx, userID)
	if err != nil || len(current) != 1 || current[0].Content != "我现在最支持的球队是皇马。" {
		t.Fatalf("current after UPDATE = %+v err=%v, want only the new claim", current, err)
	}
	history, err := records.PortraitHistory(ctx, userID)
	if err != nil || len(history) != 2 {
		t.Fatalf("history after UPDATE = %+v err=%v, want both versions", history, err)
	}
	if history[0].ID != added.ID || history[0].ValidTo.IsZero() {
		t.Fatalf("sealed row = %+v, want the 巴萨 row closed by valid_to", history[0])
	}

	// DELETE：封口 + 开放墓碑两行（同用户侧 Delete 语义），行保留（墓碑
	// 纪律）；目标不存在/已封口报 ErrNotFound。
	if _, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpDelete, TargetID: upserted.ID}, PortraitClaim{}); err != nil {
		t.Fatalf("ApplyPortraitOp DELETE: %v", err)
	}
	history, _ = records.PortraitHistory(ctx, userID)
	if len(history) != 3 {
		t.Fatalf("history after DELETE = %+v, want the sealed row plus an open tombstone", history)
	}
	tombstone := history[2]
	if !tombstone.Deleted || tombstone.Content != "" || !tombstone.ValidTo.IsZero() || tombstone.SubTopic != "user_stated" {
		t.Fatalf("tombstone = %+v, want deleted=true, empty content, open-ended, on the target's slot", tombstone)
	}
	// 遗忘槽位不因 ADD 复活：开放墓碑占住同槽，ADD 同样被拒。
	if _, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpAdd}, PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "我又有主队了。"}); !errors.Is(err, ErrSlotOccupied) {
		t.Fatalf("ADD into a tombstoned slot = %v, want ErrSlotOccupied", err)
	}
	if _, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpDelete, TargetID: upserted.ID}, PortraitClaim{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double DELETE = %v, want ErrNotFound", err)
	}

	// NOOP 不写；用户编辑路径走封口+新行，旧版本同样可回放。
	if _, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpNoop}, PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "noop"}); err != nil {
		t.Fatalf("ApplyPortraitOp NOOP: %v", err)
	}
	if _, err := records.Put(ctx, userID, "basic_info", "favorite_player", "佩德里"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := records.Put(ctx, userID, "basic_info", "favorite_player", "亚马尔"); err != nil {
		t.Fatalf("Put again: %v", err)
	}
	current, _ = records.CurrentPortrait(ctx, userID)
	// 当前视图含开放墓碑行（遮蔽合成重提取）：亚马尔新行 + 墓碑两行并存。
	if len(current) != 2 || current[1].Content != "亚马尔" || !current[0].Deleted {
		t.Fatalf("current after re-edit = %+v, want the fresh version plus the tombstone", current)
	}
	history, _ = records.PortraitHistory(ctx, userID)
	if len(history) != 5 {
		t.Fatalf("history after re-edit = %+v, want the closed 皇马 and 佩德里 rows replayable", history)
	}

	// 时间窗 SQL（③）：直接把行推出窗外，当前读法取不到、历史读法照旧。
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := records.pool.Exec(ctx, `UPDATE portrait_overlays SET valid_to = $2 WHERE user_id = $1 AND content = '亚马尔'`, userID, past); err != nil {
		t.Fatalf("seal row: %v", err)
	}
	current, _ = records.CurrentPortrait(ctx, userID)
	if len(current) != 1 || !current[0].Deleted {
		t.Fatalf("current after sealing = %+v, want only the open tombstone", current)
	}
	history, _ = records.PortraitHistory(ctx, userID)
	if len(history) != 5 {
		t.Fatalf("history after sealing = %+v, want every row regardless of window", history)
	}

	// privacy 前置纪律：活跃用户墓碑（completed）下读写一律拒绝。
	if _, err := records.pool.Exec(ctx, `INSERT INTO privacy_tombstones (user_id, status) VALUES ($1, 'completed')`, userID); err != nil {
		t.Fatalf("seed privacy tombstone: %v", err)
	}
	if err := records.Check(ctx, userID); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("Check under a completed tombstone = %v, want ErrDataDeleted", err)
	}
	if _, err := records.CurrentPortrait(ctx, userID); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("CurrentPortrait under a completed tombstone = %v, want ErrDataDeleted", err)
	}
	if _, err := records.PortraitHistory(ctx, userID); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("PortraitHistory under a completed tombstone = %v, want ErrDataDeleted", err)
	}
	if _, err := records.ApplyPortraitOp(ctx, userID, OpDecision{Op: PortraitOpAdd}, PortraitClaim{Topic: "preferences", SubTopic: "user_stated", Content: "偷写"}); !errors.Is(err, privacy.ErrDataDeleted) {
		t.Fatalf("ApplyPortraitOp under a completed tombstone = %v, want ErrDataDeleted", err)
	}
	history, _ = records.PortraitHistory(ctx, userID)
	if errors.Is(err, privacy.ErrDataDeleted) || len(history) != 5 {
		t.Fatalf("history must be untouched by refused writes, got %+v err=%v", history, err)
	}

	// 全账号隐私删除是唯一物理删例外：清掉墓碑走 DeleteUserOverlays，行真没了。
	if _, err := records.pool.Exec(ctx, `DELETE FROM privacy_tombstones WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("clear privacy tombstone: %v", err)
	}
	if err := records.DeleteUserOverlays(ctx, userID); err != nil {
		t.Fatalf("DeleteUserOverlays: %v", err)
	}
	var count int
	if err := records.pool.QueryRow(ctx, `SELECT count(*) FROM portrait_overlays WHERE user_id = $1`, userID).Scan(&count); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("count rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("rows after DeleteUserOverlays = %d, want the physical delete to remove them all", count)
	}
}
