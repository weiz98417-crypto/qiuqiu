package privacy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync/atomic"
	"time"
)

var (
	ErrDeletionInProgress = errors.New("user data deletion is in progress")
	ErrDataDeleted        = errors.New("user data has been deleted")
	ErrNotFound           = errors.New("privacy record not found")
)

type Status struct {
	UserID      string    `json:"userId,omitempty"`
	JobID       string    `json:"jobId,omitempty"`
	Status      string    `json:"status"`
	Scope       string    `json:"scope"`
	Reason      string    `json:"reason,omitempty"`
	RequestedAt time.Time `json:"requestedAt,omitempty"`
	CompletedAt time.Time `json:"completedAt,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type Export struct {
	UserID               string           `json:"userId"`
	ExportedAt           time.Time        `json:"exportedAt"`
	Sessions             []map[string]any `json:"sessions,omitempty"`
	ConversationTurns    []map[string]any `json:"conversationTurns,omitempty"`
	AgentTraces          []map[string]any `json:"agentTraces,omitempty"`
	RelationshipStates   []map[string]any `json:"relationshipStates,omitempty"`
	RelationshipMemories []map[string]any `json:"relationshipMemories,omitempty"`
	MatchStates          []map[string]any `json:"matchStates,omitempty"`
	InteractionDecisions []map[string]any `json:"interactionDecisions,omitempty"`
}

type Store interface {
	Status(context.Context, string) (Status, error)
	StatusByJob(context.Context, string) (Status, error)
	Export(context.Context, string) (Export, error)
	RequestDeletion(context.Context, string, string) (Status, error)
	ProcessDeletion(context.Context, string) error
	CleanupExpired(context.Context) error
}

type Service struct {
	store Store
}

var retentionDays atomic.Int64

func init() {
	retentionDays.Store(30)
}

func SetRetentionDays(days int) {
	if days > 0 {
		retentionDays.Store(int64(days))
	}
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Status(ctx context.Context, userID string) (Status, error) {
	return s.store.Status(ctx, normalizeUserID(userID))
}

func (s *Service) StatusByJob(ctx context.Context, jobID string) (Status, error) {
	return s.store.StatusByJob(ctx, strings.TrimSpace(jobID))
}

func (s *Service) Export(ctx context.Context, userID string) (Export, error) {
	return s.store.Export(ctx, normalizeUserID(userID))
}

func (s *Service) RequestDeletion(ctx context.Context, userID, reason string) (Status, error) {
	return s.store.RequestDeletion(ctx, normalizeUserID(userID), strings.TrimSpace(reason))
}

func (s *Service) ProcessDeletion(ctx context.Context, userID string) error {
	return s.store.ProcessDeletion(ctx, normalizeUserID(userID))
}

func (s *Service) CleanupExpired(ctx context.Context) error {
	return s.store.CleanupExpired(ctx)
}

func normalizeUserID(userID string) string {
	return strings.TrimSpace(userID)
}

func RetentionExpiry(now time.Time) time.Time {
	days := retentionDays.Load()
	if days <= 0 {
		days = 30
	}
	return now.Add(time.Duration(days) * 24 * time.Hour)
}

func NewJobID() string {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err == nil {
		return "privacy:" + hex.EncodeToString(buffer)
	}
	return "privacy:" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}
