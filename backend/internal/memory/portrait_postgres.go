package memory

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"qiuqiu/internal/privacy"
)

// Portrait overlay persistence on PostgresRecords: the local override layer
// for the C3 user page (球球懂我), layered over Memobase synthesis. The table
// comes from migrations/041_portrait_overlays.sql + 051_portrait_temporal.sql
// and every read/write honors the privacy lifecycle, exactly like
// RecordTalkativeness and the thread store.
//
// 051 之后这是一张时间化账本：id 主键行身份、valid_from/valid_to 时间窗
// （NULL=全域有效），当前视图=窗口内行，全历史保留可回放。墓碑纪律：失效
// 只封口，唯一物理删除例外是全账号隐私删除 DeleteUserOverlays。

// Check honors the privacy lifecycle: any user tombstone (completed or still
// pending) hides the whole portrait immediately.
func (r *PostgresRecords) Check(ctx context.Context, userID string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	return privacy.CheckDeletion(ctx, r.pool, userID)
}

// CurrentPortrait returns the rows inside the temporal window — the
// "next turn sees this" view — in stable (topic, sub_topic, id) order.
// Deletion tombstones stay in the result so the merge layer (ResolvePortrait)
// keeps masking synthesis slots, exactly like the pre-051 read; only the
// window is new.
func (r *PostgresRecords) CurrentPortrait(ctx context.Context, userID string) ([]PortraitOverlay, error) {
	if r == nil || r.pool == nil {
		return nil, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return nil, err
	}
	return r.queryOverlays(ctx, `
		SELECT id, topic, sub_topic, content, deleted, valid_from, valid_to, updated_at
		FROM portrait_overlays
		WHERE user_id = $1
		  AND (valid_from IS NULL OR valid_from <= now())
		  AND (valid_to IS NULL OR valid_to > now())
		ORDER BY topic, sub_topic, id
	`, userID)
}

// PortraitHistory returns every row regardless of window or tombstone, oldest
// version first — the replayable ledger behind "2026-09 之前用户支持的是 A
// 队" (explainability/debug; no API surface required).
func (r *PostgresRecords) PortraitHistory(ctx context.Context, userID string) ([]PortraitOverlay, error) {
	if r == nil || r.pool == nil {
		return nil, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return nil, err
	}
	return r.queryOverlays(ctx, `
		SELECT id, topic, sub_topic, content, deleted, valid_from, valid_to, updated_at
		FROM portrait_overlays
		WHERE user_id = $1
		ORDER BY topic, sub_topic, valid_from NULLS FIRST, id
	`, userID)
}

func (r *PostgresRecords) queryOverlays(ctx context.Context, sql, userID string) ([]PortraitOverlay, error) {
	rows, err := r.pool.Query(ctx, sql, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	overlays := make([]PortraitOverlay, 0, 8)
	for rows.Next() {
		var overlay PortraitOverlay
		var validFrom, validTo *time.Time
		if err := rows.Scan(&overlay.ID, &overlay.Topic, &overlay.SubTopic, &overlay.Content, &overlay.Deleted, &validFrom, &validTo, &overlay.UpdatedAt); err != nil {
			return nil, err
		}
		if validFrom != nil {
			overlay.ValidFrom = validFrom.UTC()
		}
		if validTo != nil {
			overlay.ValidTo = validTo.UTC()
		}
		overlay.UpdatedAt = overlay.UpdatedAt.UTC()
		overlays = append(overlays, overlay)
	}
	return overlays, rows.Err()
}

// Put records a user edit: live versions of the slot are closed (valid_to=now)
// and a fresh open-ended row opens, so re-editing a forgotten slot restores it
// while the old versions stay replayable.
func (r *PostgresRecords) Put(ctx context.Context, userID, topic, subTopic, content string) (PortraitOverlay, error) {
	if r == nil || r.pool == nil {
		return PortraitOverlay{}, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return PortraitOverlay{}, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return PortraitOverlay{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE portrait_overlays
		SET valid_to = now(), updated_at = now()
		WHERE user_id = $1 AND topic = $2 AND sub_topic = $3 AND valid_to IS NULL
	`, userID, topic, subTopic); err != nil {
		return PortraitOverlay{}, err
	}
	overlay, err := insertOverlay(ctx, tx, userID, PortraitClaim{Topic: topic, SubTopic: subTopic, Content: content})
	if err != nil {
		return PortraitOverlay{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PortraitOverlay{}, err
	}
	return overlay, nil
}

// Delete tombstones one profile slot: live versions are closed and one
// open-ended tombstone row is written. Rows are never hard-deleted here so
// Memobase re-extraction can never resurrect the fact into a prompt.
func (r *PostgresRecords) Delete(ctx context.Context, userID, topic, subTopic string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE portrait_overlays
		SET valid_to = now(), updated_at = now()
		WHERE user_id = $1 AND topic = $2 AND sub_topic = $3 AND valid_to IS NULL
	`, userID, topic, subTopic); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO portrait_overlays (user_id, topic, sub_topic, content, deleted, valid_from)
		VALUES ($1, $2, $3, '', TRUE, now())
	`, userID, topic, subTopic); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ApplyPortraitOp lands one conflict-op decision deterministically: ADD opens
// a fresh row (valid_from=now); UPDATE closes the target and opens the new
// version in one transaction; DELETE only closes the target (墓碑保留，永不
// 物理删); NOOP writes nothing. Targeting an unknown or already-closed row is
// ErrNotFound — the caller must notice the vanished target instead of
// silently writing a duplicate truth.
func (r *PostgresRecords) ApplyPortraitOp(ctx context.Context, userID string, decision OpDecision, claim PortraitClaim) (PortraitOverlay, error) {
	if r == nil || r.pool == nil {
		return PortraitOverlay{}, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return PortraitOverlay{}, err
	}
	switch decision.Op {
	case PortraitOpNoop:
		return PortraitOverlay{}, nil
	case PortraitOpAdd:
		return insertOverlay(ctx, r.pool, userID, claim)
	case PortraitOpUpdate:
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return PortraitOverlay{}, err
		}
		defer tx.Rollback(ctx)
		if err := closeOverlayRow(ctx, tx, userID, decision.TargetID); err != nil {
			return PortraitOverlay{}, err
		}
		overlay, err := insertOverlay(ctx, tx, userID, claim)
		if err != nil {
			return PortraitOverlay{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return PortraitOverlay{}, err
		}
		return overlay, nil
	case PortraitOpDelete:
		tag, err := r.pool.Exec(ctx, `
			UPDATE portrait_overlays
			SET valid_to = now(), updated_at = now()
			WHERE id = $2 AND user_id = $1 AND valid_to IS NULL
		`, userID, decision.TargetID)
		if err != nil {
			return PortraitOverlay{}, err
		}
		if tag.RowsAffected() == 0 {
			return PortraitOverlay{}, ErrNotFound
		}
		return PortraitOverlay{}, nil
	default:
		return PortraitOverlay{}, ErrNotFound
	}
}

// insertOverlay opens one fresh version (valid_from=now) and returns it with
// its ledger id. exec is either the pool or an open transaction (UPDATE 的
// 封口+新行同事务).
func insertOverlay(ctx context.Context, exec pgxExecutor, userID string, claim PortraitClaim) (PortraitOverlay, error) {
	var overlay PortraitOverlay
	var validFrom *time.Time
	err := exec.QueryRow(ctx, `
		INSERT INTO portrait_overlays (user_id, topic, sub_topic, content, deleted, valid_from)
		VALUES ($1, $2, $3, $4, FALSE, now())
		RETURNING id, topic, sub_topic, content, valid_from
	`, userID, claim.Topic, claim.SubTopic, claim.Content).Scan(
		&overlay.ID, &overlay.Topic, &overlay.SubTopic, &overlay.Content, &validFrom,
	)
	if err != nil {
		return PortraitOverlay{}, err
	}
	if validFrom != nil {
		overlay.ValidFrom = validFrom.UTC()
	}
	return overlay, nil
}

// closeOverlayRow stamps valid_to on one live row inside a transaction.
func closeOverlayRow(ctx context.Context, tx pgx.Tx, userID string, id int64) error {
	tag, err := tx.Exec(ctx, `
		UPDATE portrait_overlays
		SET valid_to = now(), updated_at = now()
		WHERE id = $2 AND user_id = $1 AND valid_to IS NULL
	`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUserOverlays removes every overlay row for the user; the full-account
// privacy deletion (ProcessDeletion) calls this so a fresh account after the
// retention window starts without stale tombstones. This physical delete is
// the one deliberate exception to the tombstone discipline.
func (r *PostgresRecords) DeleteUserOverlays(ctx context.Context, userID string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM portrait_overlays WHERE user_id = $1`, userID)
	return err
}

// pgxExecutor is the pool/transaction common ground for insertOverlay.
type pgxExecutor interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ PortraitOverlayStore = (*PostgresRecords)(nil)
	_ pgxExecutor          = (*pgxpool.Pool)(nil)
	_ pgxExecutor          = pgx.Tx(nil)
)
