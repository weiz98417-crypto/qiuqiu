package privacy

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu     sync.Mutex
	status map[string]Status
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{status: make(map[string]Status)}
}

func (s *MemoryStore) Status(_ context.Context, userID string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status, ok := s.status[userID]; ok {
		return status, nil
	}
	return Status{UserID: userID, Status: "active", Scope: "all"}, nil
}

func (s *MemoryStore) StatusByJob(_ context.Context, jobID string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, status := range s.status {
		if status.JobID == jobID {
			return status, nil
		}
	}
	return Status{}, ErrNotFound
}

func (s *MemoryStore) Export(_ context.Context, userID string) (Export, error) {
	s.mu.Lock()
	status := s.status[userID]
	s.mu.Unlock()
	if status.Status == "pending" || status.Status == "failed" {
		return Export{}, ErrDeletionInProgress
	}
	if status.Status == "completed" {
		return Export{}, ErrDataDeleted
	}
	return Export{UserID: userID, ExportedAt: time.Now().UTC()}, nil
}

func (s *MemoryStore) RequestDeletion(_ context.Context, userID, reason string) (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.status[userID]; ok && existing.Status == "completed" {
		return existing, nil
	}
	now := time.Now().UTC()
	status := Status{UserID: userID, JobID: NewJobID(), Status: "pending", Scope: "all", Reason: reason, RequestedAt: now}
	s.status[userID] = status
	return status, nil
}

func (s *MemoryStore) ProcessDeletion(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := s.status[userID]
	if status.Status == "completed" {
		return nil
	}
	status.UserID = userID
	if status.JobID == "" {
		status.JobID = NewJobID()
	}
	status.Scope = "all"
	status.Status = "completed"
	status.CompletedAt = time.Now().UTC()
	s.status[userID] = status
	return nil
}

func (s *MemoryStore) CleanupExpired(context.Context) error {
	return nil
}
