package worker_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
	"github.com/openkcm/cmk-krypton-bridge/internal/registryclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/worker"
)

// fakeOrch wraps MockClient but lets tests inject errors.
type fakeOrch struct {
	mock  *kryptonclient.MockClient
	failN int32 // fail this many times before succeeding
	calls atomic.Int32
}

func newFakeOrch(failN int) *fakeOrch {
	return &fakeOrch{mock: kryptonclient.NewMockClient(), failN: int32(failN)}
}

func (f *fakeOrch) orchestrator(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()
	return orchestrator.NewWithOptions(f.wrappedClient(), slog.Default(), true)
}

// wrappedClient returns a kryptonclient.Client that fails the first failN EnsureKey calls.
func (f *fakeOrch) wrappedClient() kryptonclient.Client {
	return &errInjectClient{mock: f.mock, failN: &f.failN, calls: &f.calls}
}

type errInjectClient struct {
	mock  *kryptonclient.MockClient
	failN *int32
	calls *atomic.Int32
}

func (c *errInjectClient) GetKey(ctx context.Context, id, tenantID string) (*kryptonclient.Key, error) {
	return c.mock.GetKey(ctx, id, tenantID)
}

func (c *errInjectClient) EnsureKey(ctx context.Context, req kryptonclient.EnsureKeyRequest) (*kryptonclient.Key, error) {
	c.calls.Add(1)
	// Decrement remaining failures; fail while count > 0.
	if remaining := atomic.AddInt32(c.failN, -1); remaining >= 0 {
		return nil, errors.New("krypton unavailable (injected)")
	}
	return c.mock.EnsureKey(ctx, req)
}

func (c *errInjectClient) ListKeysByTenant(ctx context.Context, tenantID string) ([]*kryptonclient.Key, error) {
	return c.mock.ListKeysByTenant(ctx, tenantID)
}

func (c *errInjectClient) Health(ctx context.Context) error { return nil }

func (c *errInjectClient) EnsureTenant(ctx context.Context, tenantName string) (string, error) {
	return c.mock.EnsureTenant(ctx, tenantName)
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestPool_HappyPath(t *testing.T) {
	store := jobstore.NewInMemoryJobStoreNoDelay()
	fo := newFakeOrch(0) // no failures
	orch := fo.orchestrator(t)
	reg := registryclient.NewMockClient()

	id, _ := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: "tenant-a",
		Level:    kryptonclient.LevelL2,
	})

	pool := worker.NewPool(1, store, orch, reg, slog.Default(), 10*time.Millisecond, 5*time.Minute, "eu-central", "", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go pool.Start(ctx)

	if !waitForStatus(t, store, id, jobstore.StatusDone, 2*time.Second) {
		t.Fatalf("job did not reach done within timeout")
	}

	// Registry must have been called after success.
	if !reg.TenantRegistered("tenant-a") {
		t.Error("expected tenant-a to be registered in registry after job done")
	}
}

func TestPool_RetryThenSucceed(t *testing.T) {
	store := jobstore.NewInMemoryJobStoreNoDelay()
	fo := newFakeOrch(2) // fail 2 times, succeed on 3rd
	orch := fo.orchestrator(t)

	id, _ := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: "tenant-b",
		Level:    kryptonclient.LevelL2,
	})

	pool := worker.NewPool(1, store, orch, registryclient.NoopClient{}, slog.Default(), 10*time.Millisecond, 5*time.Minute, "test-region", "", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go pool.Start(ctx)

	if !waitForStatus(t, store, id, jobstore.StatusDone, 4*time.Second) {
		job, _ := store.GetJob(context.Background(), id)
		t.Fatalf("job did not reach done — status=%q attempts=%d err=%q",
			job.Status, job.Attempts, job.LastError)
	}

	job, _ := store.GetJob(context.Background(), id)
	if job.Attempts < 2 {
		t.Errorf("expected at least 2 attempts (retries), got %d", job.Attempts)
	}
}

func TestPool_ExceedsMaxAttempts_ReachesFailed(t *testing.T) {
	store := jobstore.NewInMemoryJobStoreNoDelay()
	fo := newFakeOrch(999) // always fail
	orch := fo.orchestrator(t)

	id, err := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: "tenant-c",
		Level:    kryptonclient.LevelL2,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Override MaxAttempts to 3 so test finishes quickly.
	// InMemoryJobStore doesn't expose MaxAttempts override — work around via pool logic:
	// pool reads job.Attempts+1 >= job.MaxAttempts. We need MaxAttempts=3.
	// Set by re-enqueueing with the right field... simpler: manually adjust via All().
	for _, j := range store.All() {
		if j.ID == id {
			// Can't set fields on copy — use a thin helper on InMemoryJobStore.
			break
		}
	}
	store.SetMaxAttempts(id, 3)

	pool := worker.NewPool(1, store, orch, registryclient.NoopClient{}, slog.Default(), 5*time.Millisecond, 5*time.Minute, "test-region", "", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go pool.Start(ctx)

	if !waitForStatus(t, store, id, jobstore.StatusFailed, 4*time.Second) {
		job, _ := store.GetJob(context.Background(), id)
		t.Fatalf("job did not reach failed — status=%q attempts=%d", job.Status, job.Attempts)
	}
}

func TestPool_Heartbeat_PreventsRecovery(t *testing.T) {
	store := jobstore.NewInMemoryJobStoreNoDelay()

	// Orchestrator that blocks until told to proceed.
	proceed := make(chan struct{})
	blockingOrch := &blockingOrchestrator{proceed: proceed, mock: kryptonclient.NewMockClient()}
	orch := orchestrator.NewWithOptions(blockingOrch, slog.Default(), true)

	id, _ := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: "tenant-d",
		Level:    kryptonclient.LevelL2,
	})

	stuckTimeout := 200 * time.Millisecond
	pool := worker.NewPool(1, store, orch, registryclient.NoopClient{}, slog.Default(), 10*time.Millisecond, stuckTimeout, "test-region", "", "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go pool.Start(ctx)

	// Wait for job to be claimed.
	if !waitForStatus(t, store, id, jobstore.StatusProcessing, time.Second) {
		t.Fatal("job was not claimed")
	}

	// Wait for 3x stuckTimeout — RecoverStuck runs every stuckTimeout/2.
	// Heartbeat fires every stuckTimeout/3, so it should keep job alive.
	time.Sleep(3 * stuckTimeout)

	job, _ := store.GetJob(context.Background(), id)
	if job.Status != jobstore.StatusProcessing {
		t.Errorf("heartbeat should keep job in processing, but status=%q", job.Status)
	}

	// Unblock and let it finish.
	close(proceed)
	if !waitForStatus(t, store, id, jobstore.StatusDone, 2*time.Second) {
		t.Fatal("job did not finish after unblocking")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func waitForStatus(t *testing.T, store *jobstore.InMemoryJobStore, id string, want jobstore.Status, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		job, err := store.GetJob(context.Background(), id)
		if err == nil && job.Status == want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// blockingOrchestrator blocks EnsureKey until proceed is closed.
type blockingOrchestrator struct {
	mock    *kryptonclient.MockClient
	proceed chan struct{}
}

func (b *blockingOrchestrator) GetKey(ctx context.Context, id, tenantID string) (*kryptonclient.Key, error) {
	return b.mock.GetKey(ctx, id, tenantID)
}

func (b *blockingOrchestrator) EnsureKey(ctx context.Context, req kryptonclient.EnsureKeyRequest) (*kryptonclient.Key, error) {
	select {
	case <-b.proceed:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.mock.EnsureKey(ctx, req)
}

func (b *blockingOrchestrator) ListKeysByTenant(ctx context.Context, tenantID string) ([]*kryptonclient.Key, error) {
	return b.mock.ListKeysByTenant(ctx, tenantID)
}

func (b *blockingOrchestrator) Health(ctx context.Context) error { return nil }

func (b *blockingOrchestrator) EnsureTenant(ctx context.Context, tenantName string) (string, error) {
	return tenantName, nil
}
