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

type Request struct {
	MatchID     string
	Key         string
	PayloadHash string
	Operation   string
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
	return &Service{executor: &memoryExecutor{records: make(map[string]record)}}
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
}

type memoryExecutor struct {
	mu      sync.Mutex
	records map[string]record
}

func (e *memoryExecutor) Execute(ctx context.Context, request Request, operation Operation) (Response, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := request.MatchID + "\x00" + request.Key
	if existing, ok := e.records[key]; ok {
		if existing.PayloadHash != request.PayloadHash || existing.Operation != request.Operation {
			return Response{}, false, ErrConflict
		}
		return cloneResponse(existing.Response), true, nil
	}
	response, err := operation(ctx)
	if err != nil {
		return Response{}, false, err
	}
	e.records[key] = record{PayloadHash: request.PayloadHash, Operation: request.Operation, Response: cloneResponse(response)}
	return response, false, nil
}

func (e *memoryExecutor) Close() {}

type postgresExecutor struct {
	pool *pgxpool.Pool
}

func (e *postgresExecutor) Execute(ctx context.Context, request Request, operation Operation) (response Response, replayed bool, err error) {
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
	if _, err := tx.Exec(ctx, `
		UPDATE idempotency_records
		SET status = 'completed', response = $3, status_code = $4, updated_at = now(), last_error = ''
		WHERE match_id = $1 AND idempotency_key = $2
	`, request.MatchID, request.Key, response.Body, response.StatusCode); err != nil {
		return Response{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Response{}, false, err
	}
	return response, false, nil
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

type transactionContextKey struct{}

func Transaction(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(transactionContextKey{}).(pgx.Tx)
	return tx, ok
}
