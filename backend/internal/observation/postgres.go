package observation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/privacy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const observationColumns = `
	id, signal_id, trace_id, user_id, match_id, kind, event_type,
	claimed_team, claimed_player, claimed_score_home, claimed_score_away,
	certainty, status, candidate_fact_id, resolved_fact_id, resolved_revision,
	received_at, follow_up_deadline, reconcile_until, resolved_at, resolution_reason`

type PostgresCoordinator struct {
	pool *pgxpool.Pool
}

func OpenPostgresCoordinator(ctx context.Context, databaseURL string) (*PostgresCoordinator, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresCoordinator{pool: pool}, nil
}

func (c *PostgresCoordinator) Close() {
	if c != nil && c.pool != nil {
		c.pool.Close()
	}
}

func (c *PostgresCoordinator) Record(ctx context.Context, input Input) (PendingObservation, error) {
	input.SignalID = strings.TrimSpace(input.SignalID)
	input.UserID = strings.TrimSpace(input.UserID)
	input.MatchID = strings.TrimSpace(input.MatchID)
	if input.SignalID == "" || input.UserID == "" || input.MatchID == "" {
		return PendingObservation{}, fmt.Errorf("signalId, userId and matchId are required")
	}
	if input.ReceivedAt.IsZero() {
		input.ReceivedAt = time.Now().UTC()
	}
	input.ReceivedAt = input.ReceivedAt.UTC()
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return PendingObservation{}, err
	}
	defer tx.Rollback(ctx)
	if err := privacy.LockUserTx(ctx, tx, input.UserID); err != nil {
		return PendingObservation{}, err
	}
	if err := privacy.CheckDeletionTx(ctx, tx, input.UserID); err != nil {
		return PendingObservation{}, err
	}
	existing, err := queryObservations(ctx, tx, `
		SELECT `+observationColumns+`
		FROM pending_match_observations
		WHERE user_id = $1 AND match_id = $2
		ORDER BY received_at ASC, id ASC
		FOR UPDATE
	`, input.UserID, input.MatchID)
	if err != nil {
		return PendingObservation{}, err
	}
	for _, pending := range existing {
		if pending.SignalID == input.SignalID || sameActiveObservation(pending, input) {
			if err := tx.Commit(ctx); err != nil {
				return PendingObservation{}, err
			}
			return pending, nil
		}
	}
	active := make([]PendingObservation, 0, len(existing))
	for _, pending := range existing {
		if isActiveStatus(pending.Status) {
			active = append(active, pending)
		}
	}
	if len(active) >= 5 {
		oldest := active[0]
		resolvedAt := input.ReceivedAt
		oldest.Status = StatusSuperseded
		oldest.ResolvedAt = &resolvedAt
		oldest.ResolutionReason = "pending observation limit exceeded"
		if err := updateObservation(ctx, tx, oldest); err != nil {
			return PendingObservation{}, err
		}
	}
	followUpWindow, reconcileWindow := windowsForInput(input)
	scope := input.UserID + "\x00" + input.MatchID + "\x00" + input.SignalID
	pending := PendingObservation{
		ID: observationID(scope), SignalID: input.SignalID, TraceID: strings.TrimSpace(input.TraceID),
		UserID: input.UserID, MatchID: input.MatchID, Kind: strings.TrimSpace(input.Kind),
		EventType: strings.TrimSpace(input.EventType), ClaimedTeam: strings.TrimSpace(input.ClaimedTeam),
		ClaimedPlayer: strings.TrimSpace(input.ClaimedPlayer), ClaimedScore: cloneScore(input.ClaimedScore),
		Certainty: strings.TrimSpace(input.Certainty), Status: StatusPendingSync,
		ReceivedAt: input.ReceivedAt, FollowUpDeadline: input.ReceivedAt.Add(followUpWindow),
		ReconcileUntil: input.ReceivedAt.Add(reconcileWindow),
	}
	if err := insertObservation(ctx, tx, pending); err != nil {
		return PendingObservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PendingObservation{}, err
	}
	return pending, nil
}

func (c *PostgresCoordinator) OnFactChanged(ctx context.Context, event matchstate.MatchEvent) ([]Resolution, error) {
	if event.FactStatus != matchstate.FactStatusProvisional && event.FactStatus != matchstate.FactStatusConfirmed &&
		event.FactStatus != matchstate.FactStatusReconciled && event.FactStatus != matchstate.FactStatusRevoked {
		return nil, nil
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	users, err := observationUsers(ctx, tx, strings.TrimSpace(event.MatchID), false, time.Time{})
	if err != nil {
		return nil, err
	}
	factID := strings.TrimSpace(event.FactID)
	if factID == "" {
		factID = strings.TrimSpace(event.ID)
	}
	eventAt := matchEventTime(event)
	var resolutions []Resolution
	for _, userID := range users {
		if err := privacy.LockUserTx(ctx, tx, userID); err != nil {
			return nil, err
		}
		if err := privacy.CheckDeletionTx(ctx, tx, userID); err != nil {
			if errors.Is(err, privacy.ErrDataDeleted) || errors.Is(err, privacy.ErrDeletionInProgress) {
				continue
			}
			return nil, err
		}
		pendingRows, err := queryObservations(ctx, tx, `
			SELECT `+observationColumns+`
			FROM pending_match_observations
			WHERE match_id = $1 AND user_id = $2
			ORDER BY received_at ASC, id ASC
			FOR UPDATE
		`, event.MatchID, userID)
		if err != nil {
			return nil, err
		}
		for _, pending := range pendingRows {
			updated, changed, resolution, resolved := applyFact(pending, event, eventAt, factID)
			if changed {
				if err := updateObservation(ctx, tx, updated); err != nil {
					return nil, err
				}
			}
			if resolved {
				if strings.TrimSpace(resolution.ReliableText) != "" {
					if err := insertResolutionOutbox(ctx, tx, resolution); err != nil {
						return nil, err
					}
				}
				resolutions = append(resolutions, resolution)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return resolutions, nil
}

func (c *PostgresCoordinator) Expire(ctx context.Context, now time.Time) ([]Resolution, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	users, err := observationUsers(ctx, tx, "", true, now)
	if err != nil {
		return nil, err
	}
	var resolutions []Resolution
	for _, userID := range users {
		if err := privacy.LockUserTx(ctx, tx, userID); err != nil {
			return nil, err
		}
		if err := privacy.CheckDeletionTx(ctx, tx, userID); err != nil {
			if errors.Is(err, privacy.ErrDataDeleted) || errors.Is(err, privacy.ErrDeletionInProgress) {
				continue
			}
			return nil, err
		}
		rows, err := queryObservations(ctx, tx, `
			SELECT `+observationColumns+`
			FROM pending_match_observations
			WHERE user_id = $1 AND status IN ('pending_sync', 'corroborating', 'conflict') AND reconcile_until <= $2
			ORDER BY received_at ASC, id ASC
			FOR UPDATE
		`, userID, now)
		if err != nil {
			return nil, err
		}
		for _, pending := range rows {
			resolvedAt := now
			pending.Status = StatusExpired
			pending.ResolvedAt = &resolvedAt
			pending.ResolutionReason = "reconciliation window elapsed"
			if err := updateObservation(ctx, tx, pending); err != nil {
				return nil, err
			}
			resolutions = append(resolutions, resolutionFor(pending, matchstate.MatchEvent{}))
		}
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM pending_match_observations
		WHERE status IN ('confirmed', 'contradicted', 'expired', 'superseded')
		  AND reconcile_until <= $1
	`, now.Add(-10*time.Minute)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return resolutions, nil
}

func (c *PostgresCoordinator) Get(ctx context.Context, id string) (PendingObservation, bool, error) {
	pending, err := scanObservation(c.pool.QueryRow(ctx, `SELECT `+observationColumns+` FROM pending_match_observations WHERE id = $1`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingObservation{}, false, nil
	}
	if err != nil {
		return PendingObservation{}, false, err
	}
	return pending, true, nil
}

func (c *PostgresCoordinator) PendingResolutions(ctx context.Context, userID, matchID string, now time.Time) ([]Resolution, error) {
	userID = strings.TrimSpace(userID)
	matchID = strings.TrimSpace(matchID)
	if userID == "" || matchID == "" {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := privacy.LockUserTx(ctx, tx, userID); err != nil {
		return nil, err
	}
	if err := privacy.CheckDeletionTx(ctx, tx, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE observation_resolution_outbox
		SET status = 'suppressed'
		WHERE user_id = $1 AND match_id = $2 AND status = 'pending' AND follow_up_deadline < $3
	`, userID, matchID, now.UTC()); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT observation_id, user_id, match_id, resolution_status, fact_id,
			fact_revision, reliable_text, delivery_key, follow_up_deadline
		FROM observation_resolution_outbox
		WHERE user_id = $1 AND match_id = $2 AND status = 'pending' AND follow_up_deadline >= $3
		ORDER BY created_at ASC, delivery_key ASC
	`, userID, matchID, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var resolutions []Resolution
	for rows.Next() {
		var resolution Resolution
		var status string
		if err := rows.Scan(&resolution.ObservationID, &resolution.UserID, &resolution.MatchID, &status,
			&resolution.FactID, &resolution.FactRevision, &resolution.ReliableText,
			&resolution.DeliveryKey, &resolution.FollowUpDeadline); err != nil {
			return nil, err
		}
		resolution.Status = Status(status)
		resolution.FollowUpDeadline = resolution.FollowUpDeadline.UTC()
		resolutions = append(resolutions, resolution)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return resolutions, nil
}

func (c *PostgresCoordinator) MarkResolutionDelivered(ctx context.Context, deliveryKey string, deliveredAt time.Time) error {
	deliveryKey = strings.TrimSpace(deliveryKey)
	if deliveryKey == "" {
		return nil
	}
	if deliveredAt.IsZero() {
		deliveredAt = time.Now().UTC()
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID string
	err = tx.QueryRow(ctx, `SELECT user_id FROM observation_resolution_outbox WHERE delivery_key = $1`, deliveryKey).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := privacy.LockUserTx(ctx, tx, userID); err != nil {
		return err
	}
	if err := privacy.CheckDeletionTx(ctx, tx, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE observation_resolution_outbox
		SET status = 'delivered', delivered_at = $2
		WHERE delivery_key = $1 AND status = 'pending'
	`, deliveryKey, deliveredAt.UTC()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *PostgresCoordinator) SuppressFollowUp(ctx context.Context, observationID string, suppressedAt time.Time) error {
	observationID = strings.TrimSpace(observationID)
	if observationID == "" {
		return nil
	}
	if suppressedAt.IsZero() {
		suppressedAt = time.Now().UTC()
	}
	var userID string
	err := c.pool.QueryRow(ctx, `SELECT user_id FROM pending_match_observations WHERE id = $1`, observationID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := privacy.LockUserTx(ctx, tx, userID); err != nil {
		return err
	}
	if err := privacy.CheckDeletionTx(ctx, tx, userID); err != nil {
		if errors.Is(err, privacy.ErrDataDeleted) || errors.Is(err, privacy.ErrDeletionInProgress) {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE pending_match_observations
		SET follow_up_deadline = LEAST(follow_up_deadline, $2), updated_at = now()
		WHERE id = $1
	`, observationID, suppressedAt.UTC()); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE observation_resolution_outbox
		SET status = 'suppressed'
		WHERE observation_id = $1 AND status = 'pending'
	`, observationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *PostgresCoordinator) DeleteUser(ctx context.Context, userID string) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := privacy.LockUserTx(ctx, tx, strings.TrimSpace(userID)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pending_match_observations WHERE user_id = $1`, strings.TrimSpace(userID)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *PostgresCoordinator) ResetMatch(ctx context.Context, matchID string) error {
	_, err := c.pool.Exec(ctx, `DELETE FROM pending_match_observations WHERE match_id = $1`, strings.TrimSpace(matchID))
	return err
}

func observationUsers(ctx context.Context, tx pgx.Tx, matchID string, expiring bool, now time.Time) ([]string, error) {
	query := `SELECT DISTINCT user_id FROM pending_match_observations WHERE match_id = $1 ORDER BY user_id`
	args := []any{matchID}
	if expiring {
		query = `
			SELECT DISTINCT user_id FROM pending_match_observations
			WHERE status IN ('pending_sync', 'corroborating', 'conflict') AND reconcile_until <= $1
			ORDER BY user_id`
		args = []any{now}
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []string
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		users = append(users, userID)
	}
	sort.Strings(users)
	return users, rows.Err()
}

func insertObservation(ctx context.Context, tx pgx.Tx, pending PendingObservation) error {
	var scoreHome, scoreAway *int
	if pending.ClaimedScore != nil {
		scoreHome, scoreAway = &pending.ClaimedScore.Home, &pending.ClaimedScore.Away
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO pending_match_observations (
			id, signal_id, trace_id, user_id, match_id, kind, event_type, claimed_team, claimed_player,
			claimed_score_home, claimed_score_away, certainty, status, candidate_fact_id, resolved_fact_id,
			resolved_revision, received_at, follow_up_deadline, reconcile_until, resolved_at, resolution_reason, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,now())
	`, pending.ID, pending.SignalID, pending.TraceID, pending.UserID, pending.MatchID, pending.Kind, pending.EventType,
		pending.ClaimedTeam, pending.ClaimedPlayer, scoreHome, scoreAway, pending.Certainty, pending.Status,
		pending.CandidateFactID, pending.ResolvedFactID, pending.ResolvedRevision, pending.ReceivedAt,
		pending.FollowUpDeadline, pending.ReconcileUntil, pending.ResolvedAt, pending.ResolutionReason)
	return err
}

func updateObservation(ctx context.Context, tx pgx.Tx, pending PendingObservation) error {
	_, err := tx.Exec(ctx, `
		UPDATE pending_match_observations SET
			status = $2, candidate_fact_id = $3, resolved_fact_id = $4, resolved_revision = $5,
			resolved_at = $6, resolution_reason = $7, follow_up_deadline = $8, updated_at = now()
		WHERE id = $1
	`, pending.ID, pending.Status, pending.CandidateFactID, pending.ResolvedFactID, pending.ResolvedRevision,
		pending.ResolvedAt, pending.ResolutionReason, pending.FollowUpDeadline)
	return err
}

func insertResolutionOutbox(ctx context.Context, tx pgx.Tx, resolution Resolution) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO observation_resolution_outbox (
			delivery_key, observation_id, user_id, match_id, resolution_status,
			fact_id, fact_revision, reliable_text, follow_up_deadline, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending')
		ON CONFLICT (delivery_key) DO NOTHING
	`, resolution.DeliveryKey, resolution.ObservationID, resolution.UserID, resolution.MatchID,
		resolution.Status, resolution.FactID, resolution.FactRevision, resolution.ReliableText,
		resolution.FollowUpDeadline)
	return err
}

type rowScanner interface {
	Scan(...any) error
}

func scanObservation(row rowScanner) (PendingObservation, error) {
	var pending PendingObservation
	var status string
	var scoreHome, scoreAway *int
	err := row.Scan(
		&pending.ID, &pending.SignalID, &pending.TraceID, &pending.UserID, &pending.MatchID, &pending.Kind,
		&pending.EventType, &pending.ClaimedTeam, &pending.ClaimedPlayer, &scoreHome, &scoreAway,
		&pending.Certainty, &status, &pending.CandidateFactID, &pending.ResolvedFactID, &pending.ResolvedRevision,
		&pending.ReceivedAt, &pending.FollowUpDeadline, &pending.ReconcileUntil, &pending.ResolvedAt, &pending.ResolutionReason,
	)
	if err != nil {
		return PendingObservation{}, err
	}
	pending.Status = Status(status)
	if scoreHome != nil && scoreAway != nil {
		pending.ClaimedScore = &matchstate.Score{Home: *scoreHome, Away: *scoreAway}
	}
	pending.ReceivedAt = pending.ReceivedAt.UTC()
	pending.FollowUpDeadline = pending.FollowUpDeadline.UTC()
	pending.ReconcileUntil = pending.ReconcileUntil.UTC()
	if pending.ResolvedAt != nil {
		resolvedAt := pending.ResolvedAt.UTC()
		pending.ResolvedAt = &resolvedAt
	}
	return pending, nil
}

func queryObservations(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]PendingObservation, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var observations []PendingObservation
	for rows.Next() {
		pending, err := scanObservation(rows)
		if err != nil {
			return nil, err
		}
		observations = append(observations, pending)
	}
	return observations, rows.Err()
}
