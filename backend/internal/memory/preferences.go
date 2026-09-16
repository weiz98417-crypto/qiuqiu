package memory

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"qiuqiu/internal/privacy"
	"qiuqiu/internal/relationship"
)

// RecordTalkativeness upserts the user's 话痨程度 tier (migrations/040
// user_preferences). The backend previously dropped this field even though
// the client sent it on every user_speech payload — persisting it closes the
// drift and survives reconnects.
func (r *PostgresRecords) RecordTalkativeness(ctx context.Context, userID, tier string) error {
	if r == nil || r.pool == nil {
		return ErrUnavailable
	}
	normalized := relationship.NormalizeTalkativeness(tier)
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO user_preferences (user_id, talkativeness, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_id)
		DO UPDATE SET talkativeness = EXCLUDED.talkativeness, updated_at = now()
	`, userID, normalized)
	return err
}

// Talkativeness reads the persisted tier; unknown users degrade to "normal"
// (current behavior).
func (r *PostgresRecords) Talkativeness(ctx context.Context, userID string) (string, error) {
	if r == nil || r.pool == nil {
		return "", ErrUnavailable
	}
	if err := privacy.CheckDeletion(ctx, r.pool, userID); err != nil {
		return "", err
	}
	var tier string
	err := r.pool.QueryRow(ctx, `
		SELECT talkativeness FROM user_preferences WHERE user_id = $1
	`, userID).Scan(&tier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Unknown user: degrade to the default tier without an error.
			return relationship.TalkativenessNormal, nil
		}
		return relationship.TalkativenessNormal, err
	}
	return relationship.NormalizeTalkativeness(tier), nil
}

// compile-time guard: the preference store honors the privacy lifecycle.
var _ interface {
	RecordTalkativeness(ctx context.Context, userID, tier string) error
	Talkativeness(ctx context.Context, userID string) (string, error)
} = (*PostgresRecords)(nil)
