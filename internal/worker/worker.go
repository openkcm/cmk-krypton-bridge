package worker

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
)

// Worker polls the job store and drives each job to completion via the orchestrator.
type Worker struct {
	store        jobstore.JobStore
	orch         *orchestrator.Orchestrator
	log          *zap.Logger
	stuckTimeout time.Duration
}

// processOne claims and processes one job. No-op if the queue is empty.
func (w *Worker) processOne(ctx context.Context) {
	job, err := w.store.ClaimNext(ctx)
	if err != nil {
		w.log.Error("worker: claim next job failed", zap.Error(err))
		return
	}
	if job == nil {
		return // queue empty
	}

	w.log.Info("worker: processing job",
		zap.String("jobID", job.ID),
		zap.String("tenantID", job.TenantID),
		zap.Int("attempt", job.Attempts+1))

	// Keep updated_at fresh so RecoverStuck won't reset this in-flight job.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go func() {
		interval := w.stuckTimeout / 3
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				if hbErr := w.store.Heartbeat(hbCtx, job.ID); hbErr != nil {
					w.log.Warn("worker: heartbeat failed", zap.String("jobID", job.ID), zap.Error(hbErr))
				}
			}
		}
	}()

	result, err := w.orch.EnsureHierarchy(ctx, orchestrator.EnsureHierarchyRequest{
		TenantID:  job.TenantID,
		ServiceID: job.ServiceID,
		Level:     kryptonclient.Level(job.Level),
		Metadata:  job.Metadata,
	})
	if err != nil {
		w.log.Warn("worker: EnsureHierarchy failed",
			zap.String("jobID", job.ID),
			zap.Error(err),
			zap.Int("attempts", job.Attempts+1))

		if job.Attempts+1 >= job.MaxAttempts {
			if mfErr := w.store.MarkFailed(ctx, job.ID, err.Error()); mfErr != nil {
				w.log.Error("worker: mark failed", zap.Error(mfErr))
			}
		} else {
			if mrErr := w.store.MarkRetry(ctx, job.ID, err.Error()); mrErr != nil {
				w.log.Error("worker: mark retry", zap.Error(mrErr))
			}
		}
		return
	}

	resultKeyID := ""
	if result.RequestedKey != nil {
		resultKeyID = result.RequestedKey.ID
	}
	if err := w.store.RecordManagedKeys(ctx, job.ID, result.EnsuredKeys); err != nil {
		w.log.Error("worker: record managed keys failed", zap.String("jobID", job.ID), zap.Error(err))
		if mrErr := w.store.MarkRetry(ctx, job.ID, err.Error()); mrErr != nil {
			w.log.Error("worker: mark retry after mapping failure", zap.Error(mrErr))
		}
		return
	}
	if mdErr := w.store.MarkDone(ctx, job.ID, resultKeyID); mdErr != nil {
		w.log.Error("worker: mark done", zap.Error(mdErr))
	}

	w.log.Info("worker: job done",
		zap.String("jobID", job.ID),
		zap.String("resultKeyID", resultKeyID))
}

// Pool manages a fixed number of concurrent workers plus a stuck-job recovery routine.
type Pool struct {
	workers      int
	store        jobstore.JobStore
	orch         *orchestrator.Orchestrator
	log          *zap.Logger
	pollInterval time.Duration
	stuckTimeout time.Duration
}

// NewPool creates a Pool with the given concurrency settings.
func NewPool(
	workers int,
	store jobstore.JobStore,
	orch *orchestrator.Orchestrator,
	log *zap.Logger,
	pollInterval time.Duration,
	stuckTimeout time.Duration,
) *Pool {
	return &Pool{
		workers:      workers,
		store:        store,
		orch:         orch,
		log:          log,
		pollInterval: pollInterval,
		stuckTimeout: stuckTimeout,
	}
}

// Start launches worker goroutines and the stuck-job recovery loop.
// Blocks until ctx is cancelled.
func (p *Pool) Start(ctx context.Context) {
	var wg sync.WaitGroup

	// Launch worker goroutines.
	for i := range p.workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w := &Worker{store: p.store, orch: p.orch, log: p.log.With(zap.Int("workerID", id)), stuckTimeout: p.stuckTimeout}
			ticker := time.NewTicker(p.pollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					w.processOne(ctx)
				}
			}
		}(i)
	}

	// Stuck-job recovery runs every stuckTimeout/2.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(p.stuckTimeout / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := p.store.RecoverStuck(ctx, p.stuckTimeout)
				if err != nil {
					p.log.Error("worker: recover stuck jobs", zap.Error(err))
				} else if n > 0 {
					p.log.Info("worker: recovered stuck jobs", zap.Int("count", n))
				}
			}
		}
	}()

	wg.Wait()
}
