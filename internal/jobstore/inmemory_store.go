package jobstore

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
)

// InMemoryJobStore is a thread-safe in-memory JobStore for testing.
type InMemoryJobStore struct {
	mu             sync.Mutex
	jobs           map[string]*Job
	managedKeys    map[string]*kryptonclient.Key
	zeroRetryDelay bool // if true, MarkRetry sets retry_after to now (instant retry)
}

// NewInMemoryJobStore creates an empty store.
func NewInMemoryJobStore() *InMemoryJobStore {
	return &InMemoryJobStore{jobs: make(map[string]*Job), managedKeys: make(map[string]*kryptonclient.Key)}
}

// NewInMemoryJobStoreNoDelay creates a store where MarkRetry schedules immediate retries.
// Use this in tests that don't want to wait for exponential backoff.
func NewInMemoryJobStoreNoDelay() *InMemoryJobStore {
	return &InMemoryJobStore{jobs: make(map[string]*Job), managedKeys: make(map[string]*kryptonclient.Key), zeroRetryDelay: true}
}

func (s *InMemoryJobStore) RecordManagedKeys(ctx context.Context, jobID string, keys []*kryptonclient.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[jobID]; !ok {
		return fmt.Errorf("job %q not found", jobID)
	}
	for _, key := range keys {
		if key == nil || key.ProxyID == "" {
			return errors.New("record managed key: missing proxy ID")
		}
		keyCopy := *key
		s.managedKeys[key.ProxyID] = &keyCopy
	}
	return nil
}

// ManagedKeys returns a snapshot of the bridge's recorded key mappings.
func (s *InMemoryJobStore) ManagedKeys() []*kryptonclient.Key {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]*kryptonclient.Key, 0, len(s.managedKeys))
	for _, key := range s.managedKeys {
		keyCopy := *key
		keys = append(keys, &keyCopy)
	}
	return keys
}

func (s *InMemoryJobStore) Enqueue(ctx context.Context, req orchestrator.EnsureHierarchyRequest) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.NewString()
	s.jobs[id] = &Job{
		ID:          id,
		Status:      StatusPending,
		TenantID:    req.TenantID,
		ServiceID:   req.ServiceID,
		Level:       int(req.Level),
		Metadata:    req.Metadata,
		MaxAttempts: 10,
		RetryAfter:  time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	return id, nil
}

func (s *InMemoryJobStore) ClaimNext(ctx context.Context) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, j := range s.jobs {
		if (j.Status == StatusPending || j.Status == StatusRetrying) && !j.RetryAfter.After(now) {
			j.Status = StatusProcessing
			j.UpdatedAt = now
			cp := *j
			return &cp, nil
		}
	}
	return nil, nil //nolint:nilnil // A nil job signals an empty queue, as documented by JobStore.
}

func (s *InMemoryJobStore) Heartbeat(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	j.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryJobStore) MarkDone(ctx context.Context, id, resultKeyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	j.Status = StatusDone
	j.ResultKeyID = resultKeyID
	j.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryJobStore) MarkFailed(ctx context.Context, id, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	j.Status = StatusFailed
	j.LastError = errMsg
	j.Attempts++
	j.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryJobStore) MarkRetry(ctx context.Context, id, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	j.Attempts++
	var retryAfter time.Time
	if s.zeroRetryDelay {
		retryAfter = time.Now()
	} else {
		prev := max(j.Attempts-1, 0)
		backoff := retryBase * (1 << prev)
		jitter := time.Duration(rand.Int63n(int64(retryBase)))
		retryAfter = time.Now().Add(backoff + jitter)
	}
	j.Status = StatusRetrying
	j.LastError = errMsg
	j.RetryAfter = retryAfter
	j.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryJobStore) GetJob(ctx context.Context, id string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, fmt.Errorf("job %q not found", id)
	}
	cp := *j
	return &cp, nil
}

func (s *InMemoryJobStore) RecoverStuck(ctx context.Context, threshold time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-threshold)
	var n int
	for _, j := range s.jobs {
		if j.Status == StatusProcessing && j.UpdatedAt.Before(cutoff) {
			j.Status = StatusPending
			j.RetryAfter = time.Now()
			j.UpdatedAt = time.Now()
			n++
		}
	}
	return n, nil
}

// All returns a snapshot of all jobs (test helper).
func (s *InMemoryJobStore) All() []*Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		cp := *j
		out = append(out, &cp)
	}
	return out
}

// SetMaxAttempts overrides the MaxAttempts field for a job (test helper).
func (s *InMemoryJobStore) SetMaxAttempts(id string, maxAttempts int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j, ok := s.jobs[id]; ok {
		j.MaxAttempts = maxAttempts
	}
}

var _ JobStore = (*InMemoryJobStore)(nil)
