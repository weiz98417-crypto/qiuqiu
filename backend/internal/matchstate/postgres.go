package matchstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool        *pgxpool.Pool
	mu          sync.RWMutex
	subscribers map[string]map[chan MatchEvent]struct{}
}

func OpenPostgresStore(ctx context.Context, databaseURL, migrationsDir string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if migrationsDir != "" {
		if err := runMigrations(ctx, pool, migrationsDir); err != nil {
			pool.Close()
			return nil, err
		}
	}
	return &PostgresStore{
		pool:        pool,
		subscribers: make(map[string]map[chan MatchEvent]struct{}),
	}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) SetConfig(matchID string, config MatchConfig) (MatchConfig, Snapshot, error) {
	ctx := context.Background()
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchConfig{}, Snapshot{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	config = normalizeConfig(matchID, config)
	config.MatchID = matchID
	config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MatchConfig{}, Snapshot{}, err
	}
	defer tx.Rollback(ctx)

	kickoff := parseOptionalTime(config.Kickoff)
	_, err = tx.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, competition, kickoff, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO UPDATE SET
			home_team = EXCLUDED.home_team,
			away_team = EXCLUDED.away_team,
			competition = EXCLUDED.competition,
			kickoff = EXCLUDED.kickoff,
			updated_at = now()
	`, matchID, config.HomeTeam, config.AwayTeam, config.Competition, kickoff)
	if err != nil {
		return MatchConfig{}, Snapshot{}, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM match_players WHERE match_id = $1`, matchID); err != nil {
		return MatchConfig{}, Snapshot{}, err
	}
	if err := insertPlayers(ctx, tx, matchID, "home", config.HomeTeam, config.HomePlayers); err != nil {
		return MatchConfig{}, Snapshot{}, err
	}
	if err := insertPlayers(ctx, tx, matchID, "away", config.AwayTeam, config.AwayPlayers); err != nil {
		return MatchConfig{}, Snapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MatchConfig{}, Snapshot{}, err
	}

	return config, s.Snapshot(matchID), nil
}

func (s *PostgresStore) Config(matchID string) MatchConfig {
	ctx := context.Background()
	var config MatchConfig
	var kickoff *time.Time
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, home_team, away_team, competition, kickoff, updated_at
		FROM matches
		WHERE id = $1
	`, matchID).Scan(&config.MatchID, &config.HomeTeam, &config.AwayTeam, &config.Competition, &kickoff, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return normalizeConfig(matchID, MatchConfig{})
	}
	if err != nil {
		return normalizeConfig(matchID, MatchConfig{})
	}
	if kickoff != nil {
		config.Kickoff = kickoff.Format(time.RFC3339)
	}
	config.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	config.HomePlayers = s.players(ctx, matchID, "home")
	config.AwayPlayers = s.players(ctx, matchID, "away")
	return normalizeConfig(matchID, config)
}

func (s *PostgresStore) Create(matchID string, ev MatchEvent) (MatchEvent, Snapshot, error) {
	ctx := context.Background()
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	ev.MatchID = matchID
	normalize(&ev)
	if err := validate(ev); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	ev.ID = newEventID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ev.CreatedAt = now
	ev.UpdatedAt = now

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	defer tx.Rollback(ctx)

	if err := ensureMatch(ctx, tx, matchID, s.Config(matchID)); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertEvent(ctx, tx, ev); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertParticipants(ctx, tx, ev.ID, ev.Participants); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}

	snapshot := s.Snapshot(matchID)
	s.publish(s.subscriberList(matchID), ev)
	return ev, snapshot, nil
}

func (s *PostgresStore) Correct(matchID, eventID string, replacement MatchEvent) (MatchEvent, Snapshot, error) {
	ctx := context.Background()
	matchID = strings.TrimSpace(matchID)
	eventID = strings.TrimSpace(eventID)
	if matchID == "" || eventID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId and event id are required", ErrInvalid)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE match_events
		SET status = 'corrected', updated_at = now()
		WHERE match_id = $1 AND id = $2 AND status = 'active'
	`, matchID, eventID)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if tag.RowsAffected() == 0 {
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}

	replacement.MatchID = matchID
	replacement.RevisionOf = eventID
	normalize(&replacement)
	if err := validate(replacement); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	replacement.ID = newEventID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	replacement.CreatedAt = now
	replacement.UpdatedAt = now

	if err := insertEvent(ctx, tx, replacement); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertParticipants(ctx, tx, replacement.ID, replacement.Participants); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}

	snapshot := s.Snapshot(matchID)
	s.publish(s.subscriberList(matchID), replacement)
	return replacement, snapshot, nil
}

func (s *PostgresStore) Events(matchID string) []MatchEvent {
	events, err := s.events(context.Background(), matchID, false)
	if err != nil {
		return nil
	}
	return events
}

func (s *PostgresStore) Snapshot(matchID string) Snapshot {
	events, err := s.events(context.Background(), matchID, true)
	if err != nil {
		return buildSnapshot(matchID, nil, s.Config(matchID))
	}
	return buildSnapshot(matchID, events, s.Config(matchID))
}

func (s *PostgresStore) Subscribe(matchID string) (<-chan MatchEvent, func()) {
	ch := make(chan MatchEvent, 16)
	s.mu.Lock()
	if s.subscribers[matchID] == nil {
		s.subscribers[matchID] = make(map[chan MatchEvent]struct{})
	}
	s.subscribers[matchID][ch] = struct{}{}
	s.mu.Unlock()

	unsubscribe := func() {
		s.mu.Lock()
		if subs := s.subscribers[matchID]; subs != nil {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(s.subscribers, matchID)
			}
		}
		close(ch)
		s.mu.Unlock()
	}
	return ch, unsubscribe
}

func (s *PostgresStore) events(ctx context.Context, matchID string, ascending bool) ([]MatchEvent, error) {
	order := "DESC"
	if ascending {
		order = "ASC"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, match_id, source, provider_name, operator_id, period, clock, event_type,
			team_id, team_name, player_name, score_home, score_away, intensity, sentiment,
			description, proactive_text, tags, recommended_action, visibility, revision_of,
			status, created_at, updated_at
		FROM match_events
		WHERE match_id = $1
		ORDER BY created_at `+order+`, id `+order,
		matchID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []MatchEvent
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	if err := s.attachParticipants(ctx, events); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *PostgresStore) attachParticipants(ctx context.Context, events []MatchEvent) error {
	for i := range events {
		rows, err := s.pool.Query(ctx, `
			SELECT role, name, team_id, team_name
			FROM event_participants
			WHERE event_id = $1
			ORDER BY id ASC
		`, events[i].ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p Participant
			if err := rows.Scan(&p.Role, &p.Name, &p.TeamID, &p.TeamName); err != nil {
				rows.Close()
				return err
			}
			events[i].Participants = append(events[i].Participants, p)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	return nil
}

func (s *PostgresStore) players(ctx context.Context, matchID, teamID string) []Player {
	rows, err := s.pool.Query(ctx, `
		SELECT number, name, position
		FROM match_players
		WHERE match_id = $1 AND team_id = $2
		ORDER BY NULLIF(regexp_replace(number, '\D', '', 'g'), '')::int NULLS LAST, name ASC
	`, matchID, teamID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var players []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.Number, &p.Name, &p.Position); err != nil {
			return players
		}
		players = append(players, p)
	}
	return players
}

func (s *PostgresStore) subscriberList(matchID string) []chan MatchEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var subs []chan MatchEvent
	for ch := range s.subscribers[matchID] {
		subs = append(subs, ch)
	}
	return subs
}

func (s *PostgresStore) publish(subs []chan MatchEvent, ev MatchEvent) {
	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, file := range files {
		sql, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", filepath.Base(file), err)
		}
	}
	return nil
}

func insertPlayers(ctx context.Context, tx pgx.Tx, matchID, teamID, teamName string, players []Player) error {
	for _, player := range normalizePlayers(players) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO match_players (match_id, team_id, team_name, number, name, position)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, matchID, teamID, teamName, player.Number, player.Name, player.Position); err != nil {
			return err
		}
	}
	return nil
}

func ensureMatch(ctx context.Context, tx pgx.Tx, matchID string, config MatchConfig) error {
	config = normalizeConfig(matchID, config)
	_, err := tx.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, competition, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (id) DO NOTHING
	`, matchID, config.HomeTeam, config.AwayTeam, config.Competition)
	return err
}

func insertEvent(ctx context.Context, tx pgx.Tx, ev MatchEvent) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO match_events (
			id, match_id, source, provider_name, operator_id, period, clock, event_type,
			team_id, team_name, player_name, score_home, score_away, intensity, sentiment,
			description, proactive_text, tags, recommended_action, visibility, revision_of,
			status, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15,
			$16, $17, $18, $19, $20, $21,
			$22, $23, $24
		)
	`, ev.ID, ev.MatchID, ev.Source, ev.ProviderName, ev.OperatorID, ev.Period, ev.Clock, ev.EventType,
		ev.TeamID, ev.TeamName, ev.PlayerName, ev.Score.Home, ev.Score.Away, ev.Intensity, ev.Sentiment,
		ev.Description, ev.ProactiveText, ev.Tags, ev.RecommendedAction, ev.Visibility, nullIfEmpty(ev.RevisionOf),
		ev.Status, parseRequiredTime(ev.CreatedAt), parseRequiredTime(ev.UpdatedAt))
	return err
}

func insertParticipants(ctx context.Context, tx pgx.Tx, eventID string, participants []Participant) error {
	for _, p := range normalizeParticipants(participants) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO event_participants (event_id, role, name, team_id, team_name)
			VALUES ($1, $2, $3, $4, $5)
		`, eventID, p.Role, p.Name, p.TeamID, p.TeamName); err != nil {
			return err
		}
	}
	return nil
}

func scanEvent(rows pgx.Rows) (MatchEvent, error) {
	var ev MatchEvent
	var createdAt, updatedAt time.Time
	var revisionOf *string
	err := rows.Scan(
		&ev.ID, &ev.MatchID, &ev.Source, &ev.ProviderName, &ev.OperatorID, &ev.Period, &ev.Clock, &ev.EventType,
		&ev.TeamID, &ev.TeamName, &ev.PlayerName, &ev.Score.Home, &ev.Score.Away, &ev.Intensity, &ev.Sentiment,
		&ev.Description, &ev.ProactiveText, &ev.Tags, &ev.RecommendedAction, &ev.Visibility, &revisionOf,
		&ev.Status, &createdAt, &updatedAt,
	)
	if revisionOf != nil {
		ev.RevisionOf = *revisionOf
	}
	ev.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	ev.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return ev, err
}

func parseOptionalTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{time.RFC3339Nano, "2006-01-02T15:04", "2006-01-02 15:04:05"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return &t
		}
	}
	return nil
}

func parseRequiredTime(value string) time.Time {
	if t := parseOptionalTime(value); t != nil {
		return *t
	}
	return time.Now().UTC()
}

func nullIfEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func newEventID() string {
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}
