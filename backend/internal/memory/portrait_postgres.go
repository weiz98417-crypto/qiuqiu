package memory

import (
	"context"

	"qiuqiu/internal/privacy"
)

// Portrait overlay persistence on PostgresRecords: the local override layer
// for the C3 user page (球球懂我), layered over Memobase synthesis. The table
// comes from migrations/041_portrait_overlays.sql and every read/write honors
// the privacy lifecycle, exactly like RecordTalkativeness and the thread
// store.

// Check honors the privacy lifecycle: any user tombstone (completed or still
// pending) hides the whole portrait immediately.
func (r *PostgresRecords) Check(ctx context.Context, userID string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	return privacy.CheckDeletion(ctx, r.pool, userID)
}

// List returns the user's overlay rows (edits and tombstones) in stable
// (topic, sub_topic) order so ResolvePortrait output is reproducible.
func (r *PostgresRecords) List(ctx context.Context, userID string) ([]PortraitOverlay, error) {
	if r == nil || r.pool == nil {
		return nil, ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT topic, sub_topic, content, deleted, updated_at
		FROM portrait_overlays
		WHERE user_id = $1
		ORDER BY topic, sub_topic
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	overlays := make([]PortraitOverlay, 0, 8)
	for rows.Next() {
		var overlay PortraitOverlay
		if err := rows.Scan(&overlay.Topic, &overlay.SubTopic, &overlay.Content, &overlay.Deleted, &overlay.UpdatedAt); err != nil {
			return nil, err
		}
		overlay.UpdatedAt = overlay.UpdatedAt.UTC()
		overlays = append(overlays, overlay)
	}
	return overlays, rows.Err()
}

// Put records a user edit; the slot is marked not-deleted so re-editing a
// forgotten slot restores it.
func (r *PostgresRecords) Put(ctx context.Context, userID, topic, subTopic, content string) (PortraitOverlay, error) {
	if r == nil || r.pool == nil {
		return PortraitOverlay{}, ErrUnavailable
	}
	overlay := PortraitOverlay{Topic: topic, SubTopic: subTopic, Content: content}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return overlay, err
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO portrait_overlays (user_id, topic, sub_topic, content, deleted)
		VALUES ($1, $2, $3, $4, FALSE)
		ON CONFLICT (user_id, topic, sub_topic)
		DO UPDATE SET content = EXCLUDED.content, deleted = FALSE, updated_at = now()
		RETURNING updated_at
	`, userID, topic, subTopic, content).Scan(&overlay.UpdatedAt)
	if err != nil {
		return PortraitOverlay{}, err
	}
	overlay.UpdatedAt = overlay.UpdatedAt.UTC()
	return overlay, nil
}

// Delete tombstones one profile slot; the row stays (never hard-deleted) so
// Memobase re-extraction can never resurrect the fact into a prompt.
func (r *PostgresRecords) Delete(ctx context.Context, userID, topic, subTopic string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO portrait_overlays (user_id, topic, sub_topic, content, deleted)
		VALUES ($1, $2, $3, '', TRUE)
		ON CONFLICT (user_id, topic, sub_topic)
		DO UPDATE SET deleted = TRUE, content = '', updated_at = now()
	`, userID, topic, subTopic)
	return err
}

// DeleteUserOverlays removes every overlay row for the user; the full-account
// privacy deletion (ProcessDeletion) calls this so a fresh account after the
// retention window starts without stale tombstones.
func (r *PostgresRecords) DeleteUserOverlays(ctx context.Context, userID string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM portrait_overlays WHERE user_id = $1`, userID)
	return err
}

var _ PortraitOverlayStore = (*PostgresRecords)(nil)
