package proactive

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore 是提醒簿的生产实现（migration 045），outbox 语义与
// observation_resolution_outbox（migration 020）同款。
type PostgresStore struct {
	pool *pgxpool.Pool
}

func OpenPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("reminder store connect: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) Append(ctx context.Context, reminder Reminder) (Reminder, error) {
	reminder, err := NewReminder(reminder)
	if err != nil {
		return Reminder{}, err
	}
	row := s.pool.QueryRow(ctx, `
INSERT INTO proactive_reminders (user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, status, deliver_at, expire_at)
VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, $8)
RETURNING id`, reminder.UserID, reminder.MatchID, reminder.HomeTeam, reminder.AwayTeam,
		reminder.KickoffAt, reminder.LeadMinutes, reminder.DeliverAt, reminder.ExpireAt)
	var id int64
	if err := row.Scan(&id); err != nil {
		return Reminder{}, fmt.Errorf("reminder insert: %w", err)
	}
	reminder.ID = fmt.Sprintf("rem-%d", id)
	return reminder, nil
}

func (s *PostgresStore) PendingForUser(ctx context.Context, userID string) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, status, deliver_at, expire_at, created_at
FROM proactive_reminders
WHERE user_id = $1 AND status = 'pending'
ORDER BY deliver_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("reminder pending query: %w", err)
	}
	defer rows.Close()
	return scanReminders(rows)
}

func (s *PostgresStore) DuePending(ctx context.Context, now time.Time) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, status, deliver_at, expire_at, created_at
FROM proactive_reminders
WHERE status = 'pending' AND deliver_at <= $1 AND expire_at > $1`, now)
	if err != nil {
		return nil, fmt.Errorf("reminder due query: %w", err)
	}
	defer rows.Close()
	return scanReminders(rows)
}

func (s *PostgresStore) MarkDelivered(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE proactive_reminders SET status = 'delivered' WHERE id = $1`, numericID(id))
	if err != nil {
		return fmt.Errorf("reminder mark delivered: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("reminder %q not found", id)
	}
	return nil
}

func (s *PostgresStore) SweepSuppressed(ctx context.Context, now time.Time) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
UPDATE proactive_reminders SET status = 'suppressed'
WHERE status = 'pending' AND expire_at <= $1
RETURNING id, user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, status, deliver_at, expire_at, created_at`, now)
	if err != nil {
		return nil, fmt.Errorf("reminder sweep: %w", err)
	}
	defer rows.Close()
	return scanReminders(rows)
}

func numericID(id string) int64 {
	var numeric int64
	_, _ = fmt.Sscanf(id, "rem-%d", &numeric)
	return numeric
}

func scanReminders(rows pgx.Rows) ([]Reminder, error) {
	out := make([]Reminder, 0)
	for rows.Next() {
		var reminder Reminder
		var id int64
		var status string
		if err := rows.Scan(&id, &reminder.UserID, &reminder.MatchID, &reminder.HomeTeam, &reminder.AwayTeam,
			&reminder.KickoffAt, &reminder.LeadMinutes, &status, &reminder.DeliverAt, &reminder.ExpireAt, &reminder.CreatedAt); err != nil {
			return nil, fmt.Errorf("reminder scan: %w", err)
		}
		reminder.ID = fmt.Sprintf("rem-%d", id)
		reminder.Status = ReminderStatus(status)
		out = append(out, reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
