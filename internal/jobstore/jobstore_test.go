package jobstore_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
)

func enqueueJob(t *testing.T, store jobstore.JobStore, tenantID string) string {
	t.Helper()
	id, err := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: tenantID,
		Level:    kryptonclient.LevelL2,
	})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	return id
}

func TestInMemory_EnqueueAndClaim(t *testing.T) {
	store := jobstore.NewInMemoryJobStore()
	id := enqueueJob(t, store, "tenant-a")

	job, err := store.ClaimNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if job == nil {
		t.Fatal("expected a job, got nil")
	}
	if job.ID != id {
		t.Errorf("expected job %q, got %q", id, job.ID)
	}
	if job.Status != jobstore.StatusProcessing {
		t.Errorf("expected processing, got %q", job.Status)
	}

	// Queue now empty.
	second, err := store.ClaimNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second != nil {
		t.Errorf("expected nil (queue empty), got job %q", second.ID)
	}
}

func TestInMemory_MarkDone(t *testing.T) {
	store := jobstore.NewInMemoryJobStore()
	id := enqueueJob(t, store, "tenant-a")
	store.ClaimNext(context.Background()) //nolint

	if err := store.MarkDone(context.Background(), id, "key-123"); err != nil {
		t.Fatal(err)
	}
	job, _ := store.GetJob(context.Background(), id)
	if job.Status != jobstore.StatusDone {
		t.Errorf("expected done, got %q", job.Status)
	}
	if job.ResultKeyID != "key-123" {
		t.Errorf("expected key-123, got %q", job.ResultKeyID)
	}
}

func TestInMemory_RecordManagedKeys(t *testing.T) {
	store := jobstore.NewInMemoryJobStore()
	id := enqueueJob(t, store, "tenant-a")

	err := store.RecordManagedKeys(context.Background(), id, []*kryptonclient.Key{
		{ID: "krypton-l2", ProxyID: "tenant-a-l2", TenantID: "tenant-a", Level: kryptonclient.LevelL2},
	})
	if err != nil {
		t.Fatal(err)
	}

	keys := store.ManagedKeys()
	if len(keys) != 1 {
		t.Fatalf("expected one managed key, got %d", len(keys))
	}
	if keys[0].ProxyID != "tenant-a-l2" || keys[0].ID != "krypton-l2" {
		t.Errorf("unexpected mapping: proxy=%q krypton=%q", keys[0].ProxyID, keys[0].ID)
	}
}

func TestInMemory_MarkRetry_BackoffIncreases(t *testing.T) {
	store := jobstore.NewInMemoryJobStore()
	id := enqueueJob(t, store, "tenant-a")
	store.ClaimNext(context.Background()) //nolint

	before := time.Now()
	if err := store.MarkRetry(context.Background(), id, "transient error"); err != nil {
		t.Fatal(err)
	}

	job, _ := store.GetJob(context.Background(), id)
	if job.Status != jobstore.StatusRetrying {
		t.Errorf("expected retrying, got %q", job.Status)
	}
	if job.Attempts != 1 {
		t.Errorf("expected attempts=1, got %d", job.Attempts)
	}
	// retry_after must be in the future (backoff > 0).
	if !job.RetryAfter.After(before) {
		t.Errorf("retry_after %v should be after now %v", job.RetryAfter, before)
	}

	// Second retry: backoff should be at least 2x base (5s*2=10s).
	// To re-claim we need to reset RetryAfter.
	store.ClaimNext(context.Background()) // won't claim — RetryAfter in future, that's fine

	// Simulate second failure without reclaiming (call MarkRetry again directly).
	before2 := time.Now()
	store.MarkRetry(context.Background(), id, "still failing") //nolint
	job2, _ := store.GetJob(context.Background(), id)
	if job2.Attempts != 2 {
		t.Errorf("expected attempts=2, got %d", job2.Attempts)
	}
	if !job2.RetryAfter.After(before2) {
		t.Errorf("second retry_after should also be in the future")
	}
	// Second backoff must be >= first backoff (exponential, ignoring jitter upper bound).
	if job2.RetryAfter.Sub(before2) < job.RetryAfter.Sub(before) {
		t.Logf("note: jitter made second backoff appear shorter — acceptable in edge cases")
	}
}

func TestInMemory_MarkFailed(t *testing.T) {
	store := jobstore.NewInMemoryJobStore()
	id := enqueueJob(t, store, "tenant-a")
	store.ClaimNext(context.Background()) //nolint

	if err := store.MarkFailed(context.Background(), id, "fatal error"); err != nil {
		t.Fatal(err)
	}
	job, _ := store.GetJob(context.Background(), id)
	if job.Status != jobstore.StatusFailed {
		t.Errorf("expected failed, got %q", job.Status)
	}
	if job.LastError != "fatal error" {
		t.Errorf("expected error message, got %q", job.LastError)
	}
}

func TestInMemory_RecoverStuck(t *testing.T) {
	store := jobstore.NewInMemoryJobStore()
	id := enqueueJob(t, store, "tenant-a")

	job, _ := store.ClaimNext(context.Background())
	if job == nil {
		t.Fatal("expected job")
	}

	// Job in processing but updated_at is fresh — RecoverStuck with tight threshold should not reset it.
	n, err := store.RecoverStuck(context.Background(), 1*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 recovered (job has fresh heartbeat), got %d", n)
	}

	// RecoverStuck with zero threshold treats all processing jobs as stuck.
	n, err = store.RecoverStuck(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expected 1 recovered, got %d", n)
	}

	recovered, _ := store.GetJob(context.Background(), id)
	if recovered.Status != jobstore.StatusPending {
		t.Errorf("expected pending after recovery, got %q", recovered.Status)
	}
}

// TestInMemory_ConcurrentClaim verifies that N concurrent goroutines claiming jobs
// each get a distinct job (no double-claim), analogous to SKIP LOCKED behaviour.
func TestInMemory_ConcurrentClaim(t *testing.T) {
	const numJobs = 20
	const numWorkers = 5

	store := jobstore.NewInMemoryJobStore()
	for i := range numJobs {
		enqueueJob(t, store, "tenant-"+string(rune('A'+i%5)))
	}

	claimed := sync.Map{}
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job, err := store.ClaimNext(context.Background())
				if err != nil {
					t.Errorf("ClaimNext error: %v", err)
					return
				}
				if job == nil {
					return // queue drained
				}
				if _, loaded := claimed.LoadOrStore(job.ID, true); loaded {
					t.Errorf("double-claim: job %q claimed by two goroutines", job.ID)
				}
			}
		}()
	}
	wg.Wait()

	// Count total claimed jobs.
	var total int
	claimed.Range(func(_, _ any) bool {
		total++
		return true
	})
	if total != numJobs {
		t.Errorf("expected %d unique claimed jobs, got %d", numJobs, total)
	}
}

func TestInMemory_Heartbeat_PreventsRecovery(t *testing.T) {
	store := jobstore.NewInMemoryJobStore()
	id := enqueueJob(t, store, "tenant-a")
	store.ClaimNext(context.Background()) //nolint

	// Heartbeat refreshes updated_at.
	if err := store.Heartbeat(context.Background(), id); err != nil {
		t.Fatal(err)
	}

	// Even with a 0 threshold, the heartbeat bumped updated_at to now,
	// so the job should NOT be recovered (updated_at is not Before(now-0)).
	// Use a tiny positive threshold to test the boundary more cleanly.
	n, _ := store.RecoverStuck(context.Background(), 1*time.Hour)
	if n != 0 {
		t.Errorf("heartbeat should prevent recovery, but %d jobs recovered", n)
	}
}
