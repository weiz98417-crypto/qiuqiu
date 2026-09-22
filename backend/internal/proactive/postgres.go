package proactive

import (
	"context"
	"fmt"
	"strings"
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
INSERT INTO proactive_reminders (user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, timezone, subscription_id, status, deliver_at, expire_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'pending', $9, $10)
RETURNING id`, reminder.UserID, reminder.MatchID, reminder.HomeTeam, reminder.AwayTeam,
		reminder.KickoffAt, reminder.LeadMinutes, reminder.Timezone, reminder.SubscriptionID, reminder.DeliverAt, reminder.ExpireAt)
	var id int64
	if err := row.Scan(&id); err != nil {
		return Reminder{}, fmt.Errorf("reminder insert: %w", err)
	}
	reminder.ID = fmt.Sprintf("rem-%d", id)
	return reminder, nil
}

func (s *PostgresStore) PendingForUser(ctx context.Context, userID string) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, timezone, subscription_id, status, deliver_at, expire_at, created_at
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
SELECT id, user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, timezone, subscription_id, status, deliver_at, expire_at, created_at
FROM proactive_reminders
WHERE status = 'pending' AND deliver_at <= $1 AND expire_at > $1`, now)
	if err != nil {
		return nil, fmt.Errorf("reminder due query: %w", err)
	}
	defer rows.Close()
	return scanReminders(rows)
}

func (s *PostgresStore) MarkDelivered(ctx context.Context, id string) error {
	// pending 守卫：与 SweepSuppressed 竞态时（提醒已被翻 suppressed 并
	// 转为"错过"素材），迟到送达不得把状态改回 delivered（ADR-0015）。
	tag, err := s.pool.Exec(ctx, `UPDATE proactive_reminders SET status = 'delivered' WHERE id = $1 AND status = 'pending'`, numericID(id))
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
RETURNING id, user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, timezone, subscription_id, status, deliver_at, expire_at, created_at`, now)
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
			&reminder.KickoffAt, &reminder.LeadMinutes, &reminder.Timezone, &reminder.SubscriptionID, &status, &reminder.DeliverAt, &reminder.ExpireAt, &reminder.CreatedAt); err != nil {
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

// Active 返回全部用户的活跃订阅（每日展开器用）。
func (s *PostgresSubscriptionStore) Active(ctx context.Context) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, user_id, team_name, status, created_at FROM proactive_subscriptions
WHERE status = 'active' ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("subscription active query: %w", err)
	}
	defer rows.Close()
	out := make([]Subscription, 0)
	for rows.Next() {
		var sub Subscription
		var id int64
		if err := rows.Scan(&id, &sub.UserID, &sub.TeamName, &sub.Status, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("subscription scan: %w", err)
		}
		sub.ID = fmt.Sprintf("sub-%d", id)
		out = append(out, sub)
	}
	return out, rows.Err()
}

// AllForUser 返回该用户全部提醒（订阅展开去重用——已投递/已静默的
// 历史键也要能挡住重复展开）。
func (s *PostgresStore) AllForUser(ctx context.Context, userID string) ([]Reminder, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, user_id, match_id, home_team, away_team, kickoff_at, lead_minutes, timezone, subscription_id, status, deliver_at, expire_at, created_at
FROM proactive_reminders WHERE user_id = $1 ORDER BY deliver_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("reminder all query: %w", err)
	}
	defer rows.Close()
	return scanReminders(rows)
}

// SuppressPendingSubscription 取消订阅时静默该订阅的在途提醒（ADR-0015
// 同款静默语义：不打扰，不删除审计）。
func (s *PostgresStore) SuppressPendingSubscription(ctx context.Context, userID, subscriptionID string) error {
	_, err := s.pool.Exec(ctx, `
UPDATE proactive_reminders SET status = 'suppressed'
WHERE user_id = $1 AND subscription_id = $2 AND status = 'pending'`, userID, subscriptionID)
	if err != nil {
		return fmt.Errorf("reminder suppress subscription: %w", err)
	}
	return nil
}

// PostgresSubscriptionStore 是订阅簿的生产实现（migration 047）。
type PostgresSubscriptionStore struct {
	pool *pgxpool.Pool
}

func OpenPostgresSubscriptionStore(ctx context.Context, databaseURL string) (*PostgresSubscriptionStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("subscription store connect: %w", err)
	}
	return &PostgresSubscriptionStore{pool: pool}, nil
}

// Close 释放连接池。
func (s *PostgresSubscriptionStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresSubscriptionStore) Append(ctx context.Context, sub Subscription) (Subscription, error) {
	if strings.TrimSpace(sub.UserID) == "" || strings.TrimSpace(sub.TeamName) == "" {
		return Subscription{}, fmt.Errorf("subscription needs user and team")
	}
	var id int64
	var status string
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
INSERT INTO proactive_subscriptions (user_id, team_name)
VALUES ($1, $2)
ON CONFLICT (user_id, team_name) WHERE status = 'active'
DO UPDATE SET team_name = EXCLUDED.team_name
RETURNING id, status, created_at`, sub.UserID, sub.TeamName).Scan(&id, &status, &createdAt)
	if err != nil {
		return Subscription{}, fmt.Errorf("subscription insert: %w", err)
	}
	sub.ID = fmt.Sprintf("sub-%d", id)
	sub.Status = status
	sub.CreatedAt = createdAt
	return sub, nil
}

func (s *PostgresSubscriptionStore) ActiveForUser(ctx context.Context, userID string) ([]Subscription, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, user_id, team_name, status, created_at FROM proactive_subscriptions
WHERE user_id = $1 AND status = 'active' ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("subscription query: %w", err)
	}
	defer rows.Close()
	out := make([]Subscription, 0)
	for rows.Next() {
		var sub Subscription
		var id int64
		if err := rows.Scan(&id, &sub.UserID, &sub.TeamName, &sub.Status, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("subscription scan: %w", err)
		}
		sub.ID = fmt.Sprintf("sub-%d", id)
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (s *PostgresSubscriptionStore) CancelTeam(ctx context.Context, userID, teamName string) (Subscription, bool, error) {
	row := s.pool.QueryRow(ctx, `
UPDATE proactive_subscriptions SET status = 'cancelled'
WHERE user_id = $1 AND status = 'active' AND team_name = $2
RETURNING id, team_name, created_at`, userID, teamName)
	var sub Subscription
	var id int64
	if err := row.Scan(&id, &sub.TeamName, &sub.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return Subscription{}, false, nil
		}
		return Subscription{}, false, fmt.Errorf("subscription cancel: %w", err)
	}
	sub.ID = fmt.Sprintf("sub-%d", id)
	sub.UserID = userID
	sub.Status = SubscriptionCancelled
	return sub, true, nil
}
