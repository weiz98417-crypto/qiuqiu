package matchstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"qiuqiu/internal/operatorwrite"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool             *pgxpool.Pool
	mu               sync.RWMutex
	subscribers      map[string]map[*eventSubscription]struct{}
	clockSubscribers map[string]map[chan MatchClock]struct{}
	eventObserver    func(MatchEvent) error
	projectionAudit  func(FactProjectionAudit)
	outboxPublisher  func(MatchEvent) error
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
	store := &PostgresStore{
		pool:             pool,
		subscribers:      make(map[string]map[*eventSubscription]struct{}),
		clockSubscribers: make(map[string]map[chan MatchClock]struct{}),
	}
	store.outboxPublisher = func(event MatchEvent) error {
		store.mu.RLock()
		observer := store.eventObserver
		store.mu.RUnlock()
		if observer != nil {
			if err := observer(event); err != nil {
				return err
			}
		}
		store.publish(store.subscriberList(event.MatchID), event)
		return nil
	}
	return store, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) SetEventObserver(observer func(MatchEvent) error) {
	s.mu.Lock()
	s.eventObserver = observer
	s.mu.Unlock()
}

func (s *PostgresStore) SetFactProjectionAuditObserver(observer func(FactProjectionAudit)) {
	s.mu.Lock()
	s.projectionAudit = observer
	s.mu.Unlock()
}

func (s *PostgresStore) beginMutation(ctx context.Context) (pgx.Tx, bool, error) {
	if tx, ok := operatorwrite.Transaction(ctx); ok {
		return tx, false, nil
	}
	tx, err := s.pool.Begin(ctx)
	return tx, true, err
}

func rollbackOwnedMutation(ctx context.Context, tx pgx.Tx, owned bool) {
	if owned {
		_ = tx.Rollback(ctx)
	}
}

func commitOwnedMutation(ctx context.Context, tx pgx.Tx, owned bool) error {
	if !owned {
		return nil
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) RunOutbox(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		for {
			published, err := s.publishOutboxOnce(ctx)
			if err != nil || !published {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *PostgresStore) publishOutboxOnce(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var id int64
	var payload []byte
	var attempts int
	err = tx.QueryRow(ctx, `
		SELECT id, payload, attempts
		FROM outbox_messages
		WHERE status = 'pending' AND next_attempt_at <= now()
		ORDER BY created_at ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&id, &payload, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	attempts++
	leaseUntil := time.Now().UTC().Add(30 * time.Second)
	if _, err := tx.Exec(ctx, `
		UPDATE outbox_messages
		SET attempts = $2, next_attempt_at = $3, updated_at = now()
		WHERE id = $1
	`, id, attempts, leaseUntil); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	var event MatchEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		_, updateErr := s.pool.Exec(ctx, `
			UPDATE outbox_messages
			SET status = 'failed', last_error = $2, updated_at = now()
			WHERE id = $1
		`, id, err.Error())
		if updateErr != nil {
			return true, updateErr
		}
		return true, err
	}
	if err := s.outboxPublisher(event); err != nil {
		delay := time.Duration(1<<min(attempts-1, 6)) * time.Second
		status := "pending"
		if attempts >= 10 {
			status = "failed"
		}
		_, updateErr := s.pool.Exec(ctx, `
			UPDATE outbox_messages
			SET status = $2, next_attempt_at = $3, last_error = $4, updated_at = now()
			WHERE id = $1
		`, id, status, time.Now().UTC().Add(delay), truncateOutboxError(err.Error()))
		if updateErr != nil {
			return true, updateErr
		}
		return true, err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE outbox_messages
		SET status = 'published', published_at = now(), last_error = '', updated_at = now()
		WHERE id = $1
	`, id)
	return true, err
}

func (s *PostgresStore) kickOutbox() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = s.publishOutboxOnce(ctx)
}

func enqueueMatchEvent(ctx context.Context, tx pgx.Tx, event MatchEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	aggregateID := event.ID + ":" + strconv.Itoa(event.FactRevision) + ":" + string(event.FactStatus)
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_messages (aggregate_type, aggregate_id, event_type, payload, status, next_attempt_at, created_at, updated_at)
		VALUES ('match_event', $1, 'match_event.changed', $2, 'pending', now(), now(), now())
		ON CONFLICT (aggregate_type, aggregate_id) DO NOTHING
	`, aggregateID, payload)
	return err
}

func truncateOutboxError(value string) string {
	if len(value) > 2048 {
		return value[:2048]
	}
	return value
}

func (s *PostgresStore) SetConfig(matchID string, config MatchConfig) (MatchConfig, Snapshot, error) {
	ctx := context.Background()
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchConfig{}, Snapshot{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	automationProvided := strings.TrimSpace(config.Automation.Mode) != ""
	config = normalizeConfig(matchID, config)
	config.MatchID = matchID
	config.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	automationJSON, err := json.Marshal(config.Automation)
	if err != nil {
		return MatchConfig{}, Snapshot{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MatchConfig{}, Snapshot{}, err
	}
	defer tx.Rollback(ctx)

	kickoff := parseOptionalTime(config.Kickoff)
	_, err = tx.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, competition, kickoff, automation, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (id) DO UPDATE SET
			home_team = EXCLUDED.home_team,
			away_team = EXCLUDED.away_team,
			competition = EXCLUDED.competition,
			kickoff = EXCLUDED.kickoff,
			automation = CASE WHEN $7 THEN EXCLUDED.automation ELSE matches.automation END,
			updated_at = now()
	`, matchID, config.HomeTeam, config.AwayTeam, config.Competition, kickoff, automationJSON, automationProvided)
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

	return s.Config(matchID), s.Snapshot(matchID), nil
}

func (s *PostgresStore) SetAutomation(matchID string, policy AutomationPolicy) (AutomationPolicy, error) {
	ctx := context.Background()
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return AutomationPolicy{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	if err := validateAutomationPolicy(policy); err != nil {
		return AutomationPolicy{}, err
	}
	policy = normalizeAutomationPolicy(policy)
	automationJSON, err := json.Marshal(policy)
	if err != nil {
		return AutomationPolicy{}, err
	}
	config := s.Config(matchID)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, competition, kickoff, automation, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (id) DO UPDATE SET
			automation = EXCLUDED.automation,
			updated_at = now()
	`, matchID, config.HomeTeam, config.AwayTeam, config.Competition, parseOptionalTime(config.Kickoff), automationJSON)
	if err != nil {
		return AutomationPolicy{}, err
	}
	return policy, nil
}

func (s *PostgresStore) Config(matchID string) MatchConfig {
	ctx := context.Background()
	var config MatchConfig
	var kickoff *time.Time
	var automationJSON []byte
	var integrityJSON []byte
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, home_team, away_team, competition, kickoff, automation, integrity, updated_at
		FROM matches
		WHERE id = $1
	`, matchID).Scan(&config.MatchID, &config.HomeTeam, &config.AwayTeam, &config.Competition, &kickoff, &automationJSON, &integrityJSON, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return normalizeConfig(matchID, MatchConfig{})
	}
	if err != nil {
		return normalizeConfig(matchID, MatchConfig{})
	}
	if kickoff != nil {
		config.Kickoff = kickoff.Format(time.RFC3339)
	}
	if len(automationJSON) > 0 {
		_ = json.Unmarshal(automationJSON, &config.Automation)
	}
	if len(integrityJSON) > 0 {
		_ = json.Unmarshal(integrityJSON, &config.Integrity)
	}
	config.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	config.HomePlayers = s.players(ctx, matchID, "home")
	config.AwayPlayers = s.players(ctx, matchID, "away")
	return normalizeConfig(matchID, config)
}

func (s *PostgresStore) Clock(matchID string) MatchClock {
	matchID = strings.TrimSpace(matchID)
	clock := defaultMatchClock(matchID)
	var anchorAt *time.Time
	err := s.pool.QueryRow(context.Background(), `
		SELECT period, elapsed_seconds, running, anchor_at, source, version, updated_at
		FROM match_clocks
		WHERE match_id = $1
	`, matchID).Scan(&clock.Period, &clock.ElapsedSeconds, &clock.Running, &anchorAt, &clock.Source, &clock.Version, &clock.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) || err != nil {
		return clock
	}
	clock.AnchorAt = anchorAt
	return normalizeMatchClock(matchID, clock)
}

func (s *PostgresStore) SetClock(matchID string, command ClockCommand) (MatchClock, error) {
	ctx := context.Background()
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return MatchClock{}, fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MatchClock{}, err
	}
	defer tx.Rollback(ctx)
	if err := ensureMatch(ctx, tx, matchID, s.Config(matchID)); err != nil {
		return MatchClock{}, err
	}
	current := defaultMatchClock(matchID)
	var anchorAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT period, elapsed_seconds, running, anchor_at, source, version, updated_at
		FROM match_clocks
		WHERE match_id = $1
		FOR UPDATE
	`, matchID).Scan(&current.Period, &current.ElapsedSeconds, &current.Running, &anchorAt, &current.Source, &current.Version, &current.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return MatchClock{}, err
	}
	current.AnchorAt = anchorAt
	next, err := applyClockCommand(current, command, time.Now())
	if err != nil {
		return MatchClock{}, err
	}
	if next.Version == current.Version {
		return next, nil
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO match_clocks (match_id, period, elapsed_seconds, running, anchor_at, source, version, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (match_id) DO UPDATE SET
			period = EXCLUDED.period,
			elapsed_seconds = EXCLUDED.elapsed_seconds,
			running = EXCLUDED.running,
			anchor_at = EXCLUDED.anchor_at,
			source = EXCLUDED.source,
			version = EXCLUDED.version,
			updated_at = EXCLUDED.updated_at
	`, matchID, next.Period, next.ElapsedSeconds, next.Running, next.AnchorAt, next.Source, next.Version, next.UpdatedAt)
	if err != nil {
		return MatchClock{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MatchClock{}, err
	}
	s.publishClock(next)
	return next, nil
}

func (s *PostgresStore) SubscribeClock(matchID string) (<-chan MatchClock, func()) {
	matchID = strings.TrimSpace(matchID)
	updates := make(chan MatchClock, 1)
	s.mu.Lock()
	if s.clockSubscribers[matchID] == nil {
		s.clockSubscribers[matchID] = make(map[chan MatchClock]struct{})
	}
	s.clockSubscribers[matchID][updates] = struct{}{}
	s.mu.Unlock()
	var once sync.Once
	return updates, func() {
		once.Do(func() {
			s.mu.Lock()
			delete(s.clockSubscribers[matchID], updates)
			s.mu.Unlock()
		})
	}
}

func (s *PostgresStore) publishClock(clock MatchClock) {
	s.mu.RLock()
	subscribers := make([]chan MatchClock, 0, len(s.clockSubscribers[clock.MatchID]))
	for subscriber := range s.clockSubscribers[clock.MatchID] {
		subscribers = append(subscribers, subscriber)
	}
	s.mu.RUnlock()
	for _, subscriber := range subscribers {
		select {
		case subscriber <- clock:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- clock:
			default:
			}
		}
	}
}

func (s *PostgresStore) Reset(matchID string) error {
	ctx := context.Background()
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return fmt.Errorf("%w: matchId is required", ErrInvalid)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM matches WHERE id = $1`, matchID)
	return err
}

func (s *PostgresStore) SourceCursor(matchID, sourceType, sourceKey string) (int64, error) {
	if _, err := sourceCursorKey(matchID, sourceType, sourceKey); err != nil {
		return 0, err
	}
	var cursor int64
	err := s.pool.QueryRow(context.Background(), `
		SELECT cursor
		FROM match_source_cursors
		WHERE match_id = $1 AND source_type = $2 AND source_key = $3
	`, strings.TrimSpace(matchID), strings.TrimSpace(sourceType), strings.TrimSpace(sourceKey)).Scan(&cursor)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return cursor, err
}

func (s *PostgresStore) SetSourceCursor(matchID, sourceType, sourceKey string, cursor int64) error {
	if _, err := sourceCursorKey(matchID, sourceType, sourceKey); err != nil {
		return err
	}
	if cursor < 0 {
		return fmt.Errorf("%w: source cursor cannot be negative", ErrInvalid)
	}
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := ensureMatch(ctx, tx, strings.TrimSpace(matchID), s.Config(matchID)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO match_source_cursors (match_id, source_type, source_key, cursor, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (match_id, source_type, source_key) DO UPDATE SET
			cursor = GREATEST(match_source_cursors.cursor, EXCLUDED.cursor),
			updated_at = CASE
				WHEN EXCLUDED.cursor > match_source_cursors.cursor THEN now()
				ELSE match_source_cursors.updated_at
			END
	`, strings.TrimSpace(matchID), strings.TrimSpace(sourceType), strings.TrimSpace(sourceKey), cursor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresStore) Create(matchID string, ev MatchEvent) (MatchEvent, Snapshot, error) {
	return s.create(context.Background(), matchID, ev, false)
}

func (s *PostgresStore) CreateOperator(ctx context.Context, matchID string, ev MatchEvent) (MatchEvent, Snapshot, error) {
	return s.create(ctx, matchID, ev, true)
}

func (s *PostgresStore) create(ctx context.Context, matchID string, ev MatchEvent, publicResult bool) (MatchEvent, Snapshot, error) {
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
	if ev.FactID == "" {
		ev.FactID = ev.ID
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	ev.CreatedAt = now
	ev.UpdatedAt = now

	tx, owned, err := s.beginMutation(ctx)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	defer rollbackOwnedMutation(ctx, tx, owned)

	if err := lockMatchMutation(ctx, tx, matchID); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	config := s.Config(matchID)
	if err := ensureMatch(ctx, tx, matchID, config); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	existingEvents, err := s.events(ctx, matchID, true)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := validateEventRelations(existingEvents, ev); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := crossSourceEventError(existingEvents, ev); err != nil {
		if errors.Is(err, ErrConflict) {
			if markErr := markMatchIntegrity(ctx, tx, matchID, conflictIntegrity(ev)); markErr != nil {
				return MatchEvent{}, Snapshot{}, markErr
			}
			now := time.Now().UTC()
			if _, markErr := tx.Exec(ctx, `
				UPDATE match_events
				SET fact_status = 'conflict', confirmed = FALSE, public_at = NULL,
					fact_revision = fact_revision + 1, updated_at = $2
				WHERE match_id = $1 AND id = ANY($3)
			`, matchID, now, conflictingEventIDs(existingEvents, ev)); markErr != nil {
				return MatchEvent{}, Snapshot{}, markErr
			}
			for _, index := range crossSourceConflictIndices(existingEvents, ev) {
				existingEvents[index].FactStatus = FactStatusConflict
				existingEvents[index].Confirmed = false
				existingEvents[index].PublicAt = ""
				existingEvents[index].FactRevision++
				existingEvents[index].UpdatedAt = now.UTC().Format(time.RFC3339Nano)
				if err := insertFactRevision(ctx, tx, existingEvents[index]); err != nil {
					return MatchEvent{}, Snapshot{}, err
				}
			}
			ev.FactStatus = FactStatusConflict
			ev.Confirmed = false
			ev.CreatedAt = now.UTC().Format(time.RFC3339Nano)
			ev.UpdatedAt = ev.CreatedAt
			if err := insertEvent(ctx, tx, &ev); err != nil {
				return MatchEvent{}, Snapshot{}, err
			}
			if err := insertParticipants(ctx, tx, ev.ID, ev.Participants); err != nil {
				return MatchEvent{}, Snapshot{}, err
			}
			if err := insertFactRevision(ctx, tx, ev); err != nil {
				return MatchEvent{}, Snapshot{}, err
			}
			if err := enqueueMatchEvent(ctx, tx, ev); err != nil {
				return MatchEvent{}, Snapshot{}, err
			}
			if commitErr := commitOwnedMutation(ctx, tx, owned); commitErr != nil {
				return MatchEvent{}, Snapshot{}, commitErr
			}
			if owned {
				s.kickOutbox()
			}
		}
		return MatchEvent{}, Snapshot{}, err
	}
	if err := validateAgainstSnapshot(ev, s.Snapshot(matchID), config); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertEvent(ctx, tx, &ev); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_match_events_provider_event" {
			return MatchEvent{}, Snapshot{}, ErrDuplicate
		}
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertParticipants(ctx, tx, ev.ID, ev.Participants); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertFactRevision(ctx, tx, ev); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := enqueueMatchEvent(ctx, tx, ev); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := commitOwnedMutation(ctx, tx, owned); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	updatedEvents := append(append([]MatchEvent(nil), existingEvents...), ev)
	if publicResult {
		updatedEvents = filterPublicFacts(updatedEvents)
	}
	snapshot := buildSnapshot(matchID, updatedEvents, config, s.Clock(matchID), time.Now())
	if owned {
		s.kickOutbox()
	}
	return ev, snapshot, nil
}

func (s *PostgresStore) Correct(matchID, eventID string, replacement MatchEvent) (MatchEvent, Snapshot, error) {
	return s.correct(context.Background(), matchID, eventID, replacement, false)
}

func (s *PostgresStore) CorrectOperator(ctx context.Context, matchID, eventID string, replacement MatchEvent) (MatchEvent, Snapshot, error) {
	return s.correct(ctx, matchID, eventID, replacement, true)
}

func (s *PostgresStore) correct(ctx context.Context, matchID, eventID string, replacement MatchEvent, publicResult bool) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	eventID = strings.TrimSpace(eventID)
	if matchID == "" || eventID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId and event id are required", ErrInvalid)
	}

	tx, owned, err := s.beginMutation(ctx)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	defer rollbackOwnedMutation(ctx, tx, owned)

	if err := lockMatchMutation(ctx, tx, matchID); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	events, err := s.events(ctx, matchID, true)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	original, err := activeEventByID(events, eventID)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	replacement.MatchID = matchID
	replacement.RevisionOf = eventID
	requestedFactStatus := replacement.FactStatus
	normalize(&replacement)
	if requestedFactStatus == "" {
		replacement.FactStatus = original.FactStatus
		replacement.Confirmed = replacement.FactStatus == FactStatusConfirmed || replacement.FactStatus == FactStatusReconciled
	}
	if err := validate(replacement); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := validateCorrectionTimeline(events, original, replacement); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := validateCorrection(original, replacement, s.Snapshot(matchID), s.Config(matchID)); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}

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

	replacement.ID = newEventID()
	if replacement.FactID == "" {
		replacement.FactID = original.FactID
	}
	replacement.FactRevision = original.FactRevision + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	replacement.CreatedAt = now
	replacement.UpdatedAt = now

	if err := insertEvent(ctx, tx, &replacement); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertParticipants(ctx, tx, replacement.ID, replacement.Participants); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := insertFactRevision(ctx, tx, replacement); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := enqueueMatchEvent(ctx, tx, replacement); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := commitOwnedMutation(ctx, tx, owned); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	updatedEvents := make([]MatchEvent, 0, len(events)+1)
	for _, event := range events {
		if event.ID == eventID {
			event.Status = "corrected"
		}
		updatedEvents = append(updatedEvents, event)
	}
	updatedEvents = append(updatedEvents, replacement)
	if publicResult {
		updatedEvents = filterPublicFacts(updatedEvents)
	}
	snapshot := buildSnapshot(matchID, updatedEvents, s.Config(matchID), s.Clock(matchID), time.Now())
	if owned {
		s.kickOutbox()
	}
	return replacement, snapshot, nil
}

func lockMatchMutation(ctx context.Context, tx pgx.Tx, matchID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, matchID)
	return err
}

func markMatchIntegrity(ctx context.Context, tx pgx.Tx, matchID string, integrity MatchIntegrity) error {
	payload, err := json.Marshal(integrity)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE matches SET integrity = $2, updated_at = now() WHERE id = $1`, matchID, payload)
	return err
}

func activeEventByID(events []MatchEvent, eventID string) (MatchEvent, error) {
	for _, event := range events {
		if event.ID == eventID && event.Status == "active" {
			return event, nil
		}
	}
	return MatchEvent{}, ErrNotFound
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
		return buildSnapshot(matchID, nil, s.Config(matchID), s.Clock(matchID), time.Now())
	}
	return buildSnapshot(matchID, events, s.Config(matchID), s.Clock(matchID), time.Now())
}

func (s *PostgresStore) PublicEvents(matchID string) []MatchEvent {
	events := s.Events(matchID)
	public := make([]MatchEvent, 0, len(events))
	for _, event := range events {
		if IsPublicFact(event) {
			public = append(public, event)
		}
	}
	return public
}

func (s *PostgresStore) PublicSnapshot(matchID string) Snapshot {
	events, err := s.events(context.Background(), matchID, true)
	if err != nil {
		return buildSnapshot(matchID, nil, s.Config(matchID), s.Clock(matchID), time.Now())
	}
	config := s.Config(matchID)
	clock := s.Clock(matchID)
	now := time.Now()
	legacy := buildSnapshot(matchID, filterPublicFacts(events), config, clock, now)
	s.mu.RLock()
	observer := s.projectionAudit
	s.mu.RUnlock()
	auditFactProjection(
		FactLedgerProjectInput{MatchID: matchID, Events: events, Config: config, Clock: clock, Now: now},
		legacy,
		newestFirstPublicFacts(events),
		observer,
	)
	return legacy
}

func (s *PostgresStore) PublicSnapshotOperator(ctx context.Context, matchID string) (Snapshot, error) {
	events, err := s.events(ctx, matchID, true)
	if err != nil {
		return Snapshot{}, err
	}
	config := s.Config(matchID)
	clock := s.Clock(matchID)
	now := time.Now()
	legacy := buildSnapshot(matchID, filterPublicFacts(events), config, clock, now)
	s.mu.RLock()
	observer := s.projectionAudit
	s.mu.RUnlock()
	auditFactProjection(
		FactLedgerProjectInput{MatchID: matchID, Events: events, Config: config, Clock: clock, Now: now},
		legacy,
		newestFirstPublicFacts(events),
		observer,
	)
	return legacy, nil
}

func (s *PostgresStore) ConfirmFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.confirmFact(context.Background(), matchID, factID, operatorID, true)
}

func (s *PostgresStore) ConfirmFactOperator(ctx context.Context, matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.confirmFact(ctx, matchID, factID, operatorID, true)
}

func (s *PostgresStore) confirmFact(ctx context.Context, matchID, factID, operatorID string, publicResult bool) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	factID = strings.TrimSpace(factID)
	operatorID = strings.TrimSpace(operatorID)
	if matchID == "" || factID == "" || operatorID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId, factId and operatorId are required", ErrInvalid)
	}
	tx, owned, err := s.beginMutation(ctx)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	defer rollbackOwnedMutation(ctx, tx, owned)
	if err := lockMatchMutation(ctx, tx, matchID); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	events, err := s.events(ctx, matchID, true)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	found := -1
	for index := range events {
		if events[index].FactID == factID && events[index].Status == "active" {
			found = index
			break
		}
	}
	if found == -1 {
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}
	if events[found].FactStatus != FactStatusProvisional {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: fact is not provisional", ErrInvalid)
	}
	publicSnapshot := buildSnapshot(matchID, filterPublicFacts(events), s.Config(matchID), s.Clock(matchID), time.Now())
	if err := validateAgainstSnapshot(events[found], publicSnapshot, s.Config(matchID)); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE match_events
		SET fact_status = 'confirmed', confirmed = TRUE, confirmed_by = $3,
			public_at = $4, fact_revision = fact_revision + 1, updated_at = $4
		WHERE match_id = $1 AND fact_id = $2 AND status = 'active' AND fact_status = 'provisional'
	`, matchID, factID, operatorID, now)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if tag.RowsAffected() == 0 {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: fact is not provisional", ErrInvalid)
	}
	events[found].FactStatus = FactStatusConfirmed
	events[found].Confirmed = true
	events[found].ConfirmedBy = operatorID
	events[found].FactRevision++
	events[found].PublicAt = now.Format(time.RFC3339Nano)
	events[found].UpdatedAt = events[found].PublicAt
	if err := insertFactRevision(ctx, tx, events[found]); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := enqueueMatchEvent(ctx, tx, events[found]); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := commitOwnedMutation(ctx, tx, owned); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	snapshotEvents := events
	if publicResult {
		snapshotEvents = filterPublicFacts(events)
	}
	snapshot := buildSnapshot(matchID, snapshotEvents, s.Config(matchID), s.Clock(matchID), time.Now())
	if owned {
		s.kickOutbox()
	}
	return events[found], snapshot, nil
}

func (s *PostgresStore) RevokeFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.transitionFact(context.Background(), matchID, factID, operatorID, FactStatusRevoked, true)
}

func (s *PostgresStore) ReconcileFact(matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.transitionFact(context.Background(), matchID, factID, operatorID, FactStatusReconciled, true)
}

func (s *PostgresStore) RevokeFactOperator(ctx context.Context, matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.transitionFact(ctx, matchID, factID, operatorID, FactStatusRevoked, true)
}

func (s *PostgresStore) ReconcileFactOperator(ctx context.Context, matchID, factID, operatorID string) (MatchEvent, Snapshot, error) {
	return s.transitionFact(ctx, matchID, factID, operatorID, FactStatusReconciled, true)
}

func (s *PostgresStore) FactRevisions(matchID, factID string) []FactRevision {
	rows, err := s.pool.Query(context.Background(), `
		SELECT fact_id, revision, match_id, COALESCE(event_id, ''), status, source_type,
			source_event_id, confidence, evidence, confirmed_by, COALESCE(revision_of, ''),
			occurred_at, recorded_at, public_at
		FROM fact_revisions
		WHERE match_id = $1 AND fact_id = $2
		ORDER BY revision ASC
	`, matchID, factID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var revisions []FactRevision
	for rows.Next() {
		var revision FactRevision
		var evidenceJSON []byte
		var occurredAt, publicAt *time.Time
		var recordedAt time.Time
		if err := rows.Scan(&revision.FactID, &revision.Revision, &revision.MatchID, &revision.EventID, &revision.Status, &revision.SourceType,
			&revision.SourceEventID, &revision.Confidence, &evidenceJSON, &revision.ConfirmedBy, &revision.RevisionOf,
			&occurredAt, &recordedAt, &publicAt); err != nil {
			return revisions
		}
		if len(evidenceJSON) > 0 {
			_ = json.Unmarshal(evidenceJSON, &revision.Evidence)
		}
		if occurredAt != nil {
			revision.OccurredAt = occurredAt.UTC().Format(time.RFC3339Nano)
		}
		revision.RecordedAt = recordedAt.UTC().Format(time.RFC3339Nano)
		if publicAt != nil {
			revision.PublicAt = publicAt.UTC().Format(time.RFC3339Nano)
		}
		revisions = append(revisions, revision)
	}
	return revisions
}

func (s *PostgresStore) transitionFact(ctx context.Context, matchID, factID, operatorID string, status FactStatus, publicResult bool) (MatchEvent, Snapshot, error) {
	matchID = strings.TrimSpace(matchID)
	factID = strings.TrimSpace(factID)
	operatorID = strings.TrimSpace(operatorID)
	if matchID == "" || factID == "" || operatorID == "" {
		return MatchEvent{}, Snapshot{}, fmt.Errorf("%w: matchId, factId and operatorId are required", ErrInvalid)
	}
	tx, owned, err := s.beginMutation(ctx)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	defer rollbackOwnedMutation(ctx, tx, owned)
	if err := lockMatchMutation(ctx, tx, matchID); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	events, err := s.events(ctx, matchID, true)
	if err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	found := -1
	for index := range events {
		if events[index].FactID == factID && events[index].Status == "active" {
			found = index
			break
		}
	}
	if found == -1 {
		return MatchEvent{}, Snapshot{}, ErrNotFound
	}
	if err := validateFactTransition(events[found].FactStatus, status); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	now := time.Now().UTC()
	confirmed := status == FactStatusReconciled
	publicAt := any(nil)
	if confirmed {
		publicAt = now
	}
	if _, err := tx.Exec(ctx, `
		UPDATE match_events
		SET fact_status = $3, confirmed = $4, confirmed_by = $5,
			public_at = $6, fact_revision = fact_revision + 1, updated_at = $7
		WHERE match_id = $1 AND fact_id = $2 AND status = 'active'
	`, matchID, factID, status, confirmed, operatorID, publicAt, now); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	events[found].FactStatus = status
	events[found].Confirmed = confirmed
	events[found].ConfirmedBy = operatorID
	events[found].PublicAt = ""
	if confirmed {
		events[found].PublicAt = now.Format(time.RFC3339Nano)
	}
	events[found].FactRevision++
	events[found].UpdatedAt = now.Format(time.RFC3339Nano)
	if err := insertFactRevision(ctx, tx, events[found]); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if status == FactStatusReconciled {
		for index := range events {
			if index == found || events[index].Status != "active" || events[index].FactStatus != FactStatusConflict {
				continue
			}
			if events[index].EventType != events[found].EventType || events[index].Period != events[found].Period || !clocksNear(events[index].Clock, events[found].Clock, 45) {
				continue
			}
			if _, err := tx.Exec(ctx, `
				UPDATE match_events
				SET fact_status = 'revoked', confirmed = FALSE, confirmed_by = $3,
					public_at = NULL, fact_revision = fact_revision + 1, updated_at = $4
				WHERE match_id = $1 AND id = $2 AND status = 'active'
			`, matchID, events[index].ID, operatorID, now); err != nil {
				return MatchEvent{}, Snapshot{}, err
			}
			events[index].FactStatus = FactStatusRevoked
			events[index].Confirmed = false
			events[index].ConfirmedBy = operatorID
			events[index].PublicAt = ""
			events[index].FactRevision++
			events[index].UpdatedAt = now.Format(time.RFC3339Nano)
			if err := insertFactRevision(ctx, tx, events[index]); err != nil {
				return MatchEvent{}, Snapshot{}, err
			}
		}
		if err := markMatchIntegrity(ctx, tx, matchID, MatchIntegrity{Status: "ok"}); err != nil {
			return MatchEvent{}, Snapshot{}, err
		}
	}
	if err := enqueueMatchEvent(ctx, tx, events[found]); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	if err := commitOwnedMutation(ctx, tx, owned); err != nil {
		return MatchEvent{}, Snapshot{}, err
	}
	changed := events[found]
	snapshotEvents := events
	if publicResult {
		snapshotEvents = filterPublicFacts(events)
	}
	snapshot := buildSnapshot(matchID, snapshotEvents, s.Config(matchID), s.Clock(matchID), time.Now())
	if owned {
		s.kickOutbox()
	}
	return changed, snapshot, nil
}

func (s *PostgresStore) Subscribe(matchID string) (<-chan MatchEvent, func()) {
	subscription := newEventSubscription()
	s.mu.Lock()
	if s.subscribers[matchID] == nil {
		s.subscribers[matchID] = make(map[*eventSubscription]struct{})
	}
	s.subscribers[matchID][subscription] = struct{}{}
	s.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			if subs := s.subscribers[matchID]; subs != nil {
				delete(subs, subscription)
				if len(subs) == 0 {
					delete(s.subscribers, matchID)
				}
			}
			s.mu.Unlock()
			subscription.close()
		})
	}
	return subscription.events, unsubscribe
}

func (s *PostgresStore) events(ctx context.Context, matchID string, ascending bool) ([]MatchEvent, error) {
	order := "DESC"
	if ascending {
		order = "ASC"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(fact_id, id), id, match_id, source, provider_name, provider_event_id, operator_id, period, clock, event_type,
			team_id, team_name, player_name, score_home, score_away, intensity, confirmed, sentiment,
			description, proactive_text, tags, recommended_action, visibility, revision_of,
			status, fact_revision, fact_status, confidence, evidence, confirmed_by, public_at, created_at, updated_at, recorded_sequence
		FROM match_events
		WHERE match_id = $1
		ORDER BY recorded_sequence `+order,
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

func (s *PostgresStore) subscriberList(matchID string) []*eventSubscription {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var subs []*eventSubscription
	for subscription := range s.subscribers[matchID] {
		subs = append(subs, subscription)
	}
	return subs
}

func (s *PostgresStore) publish(subs []*eventSubscription, ev MatchEvent) {
	for _, subscription := range subs {
		subscription.enqueue(ev)
	}
}

func runMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('qiuqiu_schema_migrations', 0))`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}
	for _, file := range files {
		name := filepath.Base(file)
		var applied bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		sql, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (name) VALUES ($1)`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return tx.Commit(ctx)
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
	automationJSON, err := json.Marshal(config.Automation)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO matches (id, home_team, away_team, competition, automation, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (id) DO NOTHING
	`, matchID, config.HomeTeam, config.AwayTeam, config.Competition, automationJSON)
	return err
}

func insertEvent(ctx context.Context, tx pgx.Tx, ev *MatchEvent) error {
	evidence, err := json.Marshal(ev.Evidence)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO match_events (
			id, match_id, source, provider_name, provider_event_id, operator_id, period, clock, event_type,
			team_id, team_name, player_name, score_home, score_away, intensity, confirmed, sentiment,
			description, proactive_text, tags, recommended_action, visibility, revision_of,
			status, fact_id, fact_revision, fact_status, confidence, evidence, confirmed_by, public_at, created_at, updated_at
		)
		VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			$10, $11, $12, $13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22, $23,
			$24, $25, $26, $27, $28, $29, $30, $31, $32, $33
		)
		RETURNING recorded_sequence
	`, ev.ID, ev.MatchID, ev.Source, ev.ProviderName, ev.ProviderEventID, ev.OperatorID, ev.Period, ev.Clock, ev.EventType,
		ev.TeamID, ev.TeamName, ev.PlayerName, ev.Score.Home, ev.Score.Away, ev.Intensity, ev.Confirmed, ev.Sentiment,
		ev.Description, ev.ProactiveText, ev.Tags, ev.RecommendedAction, ev.Visibility, nullIfEmpty(ev.RevisionOf),
		ev.Status, ev.FactID, ev.FactRevision, ev.FactStatus, ev.Confidence, evidence, ev.ConfirmedBy, parseOptionalTime(ev.PublicAt), parseRequiredTime(ev.CreatedAt), parseRequiredTime(ev.UpdatedAt)).Scan(&ev.RecordedSequence)
	return err
}

func insertFactRevision(ctx context.Context, tx pgx.Tx, ev MatchEvent) error {
	evidence, err := json.Marshal(ev.Evidence)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO fact_revisions (
			fact_id, revision, match_id, event_id, status, source_type, source_event_id,
			confidence, evidence, confirmed_by, revision_of, occurred_at, public_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (fact_id, revision) DO NOTHING
	`, ev.FactID, ev.FactRevision, ev.MatchID, nullIfEmpty(ev.ID), ev.FactStatus, factSourceType(ev.Source), ev.ProviderEventID,
		ev.Confidence, evidence, ev.ConfirmedBy, nullIfEmpty(ev.RevisionOf), parseOptionalTime(ev.CreatedAt), parseOptionalTime(ev.PublicAt))
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
	var evidenceJSON []byte
	var publicAt *time.Time
	err := rows.Scan(
		&ev.FactID, &ev.ID, &ev.MatchID, &ev.Source, &ev.ProviderName, &ev.ProviderEventID, &ev.OperatorID, &ev.Period, &ev.Clock, &ev.EventType,
		&ev.TeamID, &ev.TeamName, &ev.PlayerName, &ev.Score.Home, &ev.Score.Away, &ev.Intensity, &ev.Confirmed, &ev.Sentiment,
		&ev.Description, &ev.ProactiveText, &ev.Tags, &ev.RecommendedAction, &ev.Visibility, &revisionOf,
		&ev.Status, &ev.FactRevision, &ev.FactStatus, &ev.Confidence, &evidenceJSON, &ev.ConfirmedBy, &publicAt, &createdAt, &updatedAt, &ev.RecordedSequence,
	)
	if revisionOf != nil {
		ev.RevisionOf = *revisionOf
	}
	ev.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	ev.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	if len(evidenceJSON) > 0 {
		_ = json.Unmarshal(evidenceJSON, &ev.Evidence)
	}
	if publicAt != nil {
		ev.PublicAt = publicAt.UTC().Format(time.RFC3339Nano)
	}
	normalizeFactMetadata(&ev)
	if ev.FactID == "" {
		ev.FactID = ev.ID
	}
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
