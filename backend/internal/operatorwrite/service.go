package operatorwrite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrKeyRequired = errors.New("Idempotency-Key is required")
	ErrConflict    = errors.New("idempotency key was already used with a different payload")
	ErrInProgress  = errors.New("idempotent operation is still in progress")
)

const reservedOperationLease = 2 * time.Minute
const memoryCleanupInterval = time.Minute

type Request struct {
	MatchID     string
	Key         string
	PayloadHash string
	Operation   string
	Atomic      bool
}

type Response struct {
	StatusCode int
	Body       []byte
}

type Operation func(context.Context) (Response, error)

type executor interface {
	Execute(context.Context, Request, Operation) (Response, bool, error)
	Close()
}

type Service struct {
	executor executor
}

func NewMemoryService() *Service {
	return newMemoryService(24*time.Hour, time.Now)
}

func newMemoryService(ttl time.Duration, now func() time.Time) *Service {
	return &Service{executor: &memoryExecutor{
		records: make(map[string]record),
		ttl:     ttl,
		now:     now,
	}}
}

func OpenPostgresService(ctx context.Context, databaseURL string) (*Service, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Service{executor: &postgresExecutor{pool: pool}}, nil
}

func (s *Service) Execute(ctx context.Context, request Request, operation Operation) (Response, bool, error) {
	request.MatchID = strings.TrimSpace(request.MatchID)
	request.Key = strings.TrimSpace(request.Key)
	request.PayloadHash = strings.TrimSpace(request.PayloadHash)
	request.Operation = strings.TrimSpace(request.Operation)
	if request.Key == "" {
		return Response{}, false, ErrKeyRequired
	}
	if len(request.Key) > 200 {
		return Response{}, false, fmt.Errorf("%w: key is too long", ErrKeyRequired)
	}
	if request.MatchID == "" || request.PayloadHash == "" || request.Operation == "" {
		return Response{}, false, errors.New("invalid idempotency request")
	}
	return s.executor.Execute(ctx, request, operation)
}

func (s *Service) Close() {
	if s != nil && s.executor != nil {
		s.executor.Close()
	}
}

func JSONResponse(statusCode int, payload any) (Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, err
	}
	return Response{StatusCode: statusCode, Body: body}, nil
}

func PayloadHash(method, path string, body []byte) (string, error) {
	canonicalBody := []byte("null")
	if strings.TrimSpace(string(body)) != "" {
		var payload any
		if err := json.Unmarshal(body, &payload); err != nil {
			return "", err
		}
		var err error
		canonicalBody, err = json.Marshal(payload)
		if err != nil {
			return "", err
		}
	}
	digest := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(method)) + "\n" + strings.TrimSpace(path) + "\n" + string(canonicalBody)))
	return hex.EncodeToString(digest[:]), nil
}

type record struct {
	PayloadHash string
	Operation   string
	Response    Response
	ExpiresAt   time.Time
}

type memoryExecutor struct {
	mu          sync.Mutex
	records     map[string]record
	ttl         time.Duration
	now         func() time.Time
	lastCleanup time.Time
}

func (e *memoryExecutor) Execute(ctx context.Context, request Request, operation Operation) (Response, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.now().UTC()
	e.cleanupExpired(now)
	key := request.MatchID + "\x00" + request.Key
	if existing, ok := e.records[key]; ok {
		if !existing.ExpiresAt.After(now) {
			delete(e.records, key)
		} else {
			if existing.PayloadHash != request.PayloadHash || existing.Operation != request.Operation {
				return Response{}, false, ErrConflict
			}
			return cloneResponse(existing.Response), true, nil
		}
	}
	response, err := operation(ctx)
	if err != nil {
		return Response{}, false, err
	}
	e.records[key] = record{PayloadHash: request.PayloadHash, Operation: request.Operation, Response: cloneResponse(response), ExpiresAt: now.Add(e.ttl)}
	return response, false, nil
}

func (e *memoryExecutor) cleanupExpired(now time.Time) {
	if !e.lastCleanup.IsZero() && now.Sub(e.lastCleanup) < memoryCleanupInterval {
		return
	}
	for key, existing := range e.records {
		if !existing.ExpiresAt.After(now) {
			delete(e.records, key)
		}
	}
	e.lastCleanup = now
}

func (e *memoryExecutor) Close() {}

type postgresExecutor struct {
	pool *pgxpool.Pool
}

func (e *postgresExecutor) Execute(ctx context.Context, request Request, operation Operation) (response Response, replayed bool, err error) {
	if !request.Atomic {
		return e.executeReserved(ctx, request, operation)
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return Response{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "operator-write:"+request.MatchID+":"+request.Key); err != nil {
		return Response{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM idempotency_records
		WHERE ctid IN (
			SELECT ctid FROM idempotency_records
			WHERE expires_at <= now()
			ORDER BY expires_at ASC
			LIMIT 100
		)
	`); err != nil {
		return Response{}, false, err
	}
	var payloadHash, operationName, status string
	var storedBody []byte
	var statusCode int
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT payload_hash, operation, status, response, status_code, expires_at
		FROM idempotency_records
		WHERE match_id = $1 AND idempotency_key = $2
	`, request.MatchID, request.Key).Scan(&payloadHash, &operationName, &status, &storedBody, &statusCode, &expiresAt)
	if err == nil && !expiresAt.After(time.Now().UTC()) {
		if _, err := tx.Exec(ctx, `DELETE FROM idempotency_records WHERE match_id = $1 AND idempotency_key = $2`, request.MatchID, request.Key); err != nil {
			return Response{}, false, err
		}
		err = pgx.ErrNoRows
	}
	if err == nil {
		if payloadHash != request.PayloadHash || operationName != request.Operation {
			return Response{}, false, ErrConflict
		}
		if status != "completed" {
			return Response{}, false, ErrInProgress
		}
		return Response{StatusCode: statusCode, Body: append([]byte(nil), storedBody...)}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Response{}, false, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		INSERT INTO idempotency_records (
			match_id, idempotency_key, payload_hash, operation, status, created_at, updated_at, expires_at
		) VALUES ($1, $2, $3, $4, 'pending', $5, $5, $6)
	`, request.MatchID, request.Key, request.PayloadHash, request.Operation, now, now.Add(24*time.Hour)); err != nil {
		return Response{}, false, err
	}
	response, err = operation(context.WithValue(ctx, transactionContextKey{}, tx))
	if err != nil {
		return Response{}, false, err
	}
	if !json.Valid(response.Body) {
		return Response{}, false, errors.New("idempotent response must be valid json")
	}
	resultEventID := responseEventID(response.Body)
	if _, err := tx.Exec(ctx, `
		UPDATE idempotency_records
		SET status = 'completed', response = $3, status_code = $4, result_event_id = NULLIF($5, ''), updated_at = now(), last_error = ''
		WHERE match_id = $1 AND idempotency_key = $2
	`, request.MatchID, request.Key, response.Body, response.StatusCode, resultEventID); err != nil {
		return Response{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Response{}, false, err
	}
	return response, false, nil
}

func (e *postgresExecutor) executeReserved(ctx context.Context, request Request, operation Operation) (Response, bool, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return Response{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "operator-write:"+request.MatchID+":"+request.Key); err != nil {
		return Response{}, false, err
	}
	var payloadHash, operationName, status string
	var storedBody []byte
	var statusCode int
	var expiresAt, updatedAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT payload_hash, operation, status, response, status_code, expires_at, updated_at
		FROM idempotency_records
		WHERE match_id = $1 AND idempotency_key = $2
	`, request.MatchID, request.Key).Scan(&payloadHash, &operationName, &status, &storedBody, &statusCode, &expiresAt, &updatedAt)
	now := time.Now().UTC()
	if err == nil && !expiresAt.After(now) {
		if _, err := tx.Exec(ctx, `DELETE FROM idempotency_records WHERE match_id = $1 AND idempotency_key = $2`, request.MatchID, request.Key); err != nil {
			return Response{}, false, err
		}
		err = pgx.ErrNoRows
	}
	if err == nil {
		if payloadHash != request.PayloadHash || operationName != request.Operation {
			return Response{}, false, ErrConflict
		}
		if status == "completed" {
			return Response{StatusCode: statusCode, Body: append([]byte(nil), storedBody...)}, true, nil
		}
		if status == "pending" && updatedAt.Add(reservedOperationLease).After(now) {
			return Response{}, false, ErrInProgress
		}
		if _, err := tx.Exec(ctx, `
			UPDATE idempotency_records
			SET status = 'pending', response = NULL, status_code = 0, result_event_id = NULL,
				updated_at = $3, expires_at = $4, last_error = ''
			WHERE match_id = $1 AND idempotency_key = $2
		`, request.MatchID, request.Key, now, now.Add(24*time.Hour)); err != nil {
			return Response{}, false, err
		}
	} else if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO idempotency_records (
				match_id, idempotency_key, payload_hash, operation, status, created_at, updated_at, expires_at
			) VALUES ($1, $2, $3, $4, 'pending', $5, $5, $6)
		`, request.MatchID, request.Key, request.PayloadHash, request.Operation, now, now.Add(24*time.Hour)); err != nil {
			return Response{}, false, err
		}
	} else {
		return Response{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Response{}, false, err
	}

	response, err := operation(ctx)
	if err != nil {
		return Response{}, false, e.failReserved(ctx, request, err)
	}
	if !json.Valid(response.Body) {
		return Response{}, false, e.failReserved(ctx, request, errors.New("idempotent response must be valid json"))
	}
	resultEventID := responseEventID(response.Body)
	completionCtx, completionCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer completionCancel()
	commandTag, err := e.pool.Exec(completionCtx, `
		UPDATE idempotency_records
		SET status = 'completed', response = $3, status_code = $4, result_event_id = NULLIF($5, ''), updated_at = now(), last_error = ''
		WHERE match_id = $1 AND idempotency_key = $2 AND status = 'pending'
	`, request.MatchID, request.Key, response.Body, response.StatusCode, resultEventID)
	if err != nil {
		return Response{}, false, err
	}
	if commandTag.RowsAffected() != 1 {
		return Response{}, false, errors.New("idempotency reservation was lost before completion")
	}
	return response, false, nil
}

func (e *postgresExecutor) failReserved(ctx context.Context, request Request, cause error) error {
	message := strings.TrimSpace(cause.Error())
	if len(message) > 2000 {
		message = message[:2000]
	}
	failureCtx, failureCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer failureCancel()
	if _, err := e.pool.Exec(failureCtx, `
		UPDATE idempotency_records
		SET status = 'failed', updated_at = now(), last_error = $3
		WHERE match_id = $1 AND idempotency_key = $2 AND status = 'pending'
	`, request.MatchID, request.Key, message); err != nil {
		return fmt.Errorf("%w; persist idempotency failure: %v", cause, err)
	}
	return cause
}

func (e *postgresExecutor) Close() {
	if e != nil && e.pool != nil {
		e.pool.Close()
	}
}

func cloneResponse(response Response) Response {
	response.Body = append([]byte(nil), response.Body...)
	return response
}

func responseEventID(body []byte) string {
	var envelope struct {
		Event struct {
			ID string `json:"id"`
		} `json:"event"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	return strings.TrimSpace(envelope.Event.ID)
}

type transactionContextKey struct{}

func Transaction(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(transactionContextKey{}).(pgx.Tx)
	return tx, ok
}
