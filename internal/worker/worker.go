package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
	"github.com/openkcm/cmk-krypton-bridge/internal/registryclient"
)

// Worker polls the job store and drives each job to completion via the orchestrator.
type Worker struct {
	store        jobstore.JobStore
	orch         *orchestrator.Orchestrator
	registry     registryclient.Client
	log          *slog.Logger
	stuckTimeout time.Duration
	region       string
	ownerID      string
	ownerType    string
	systemType   string
	role         string
}

// processOne claims and processes one job. No-op if the queue is empty.
func (w *Worker) processOne(ctx context.Context) {
	job, err := w.store.ClaimNext(ctx)
	if err != nil {
		w.log.ErrorContext(ctx, "worker: claim next job failed", slog.String("error", err.Error()))
		return
	}
	if job == nil {
		return // queue empty
	}

	w.log.InfoContext(ctx, "worker: processing job",
		slog.String("jobID", job.ID),
		slog.String("tenantID", job.TenantID),
		slog.Int("attempt", job.Attempts+1))

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
					w.log.WarnContext(hbCtx, "worker: heartbeat failed", slog.String("jobID", job.ID), slog.String("error", hbErr.Error()))
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
		w.log.WarnContext(ctx, "worker: EnsureHierarchy failed",
			slog.String("jobID", job.ID),
			slog.String("error", err.Error()),
			slog.Int("attempts", job.Attempts+1))

		if job.Attempts+1 >= job.MaxAttempts {
			if mfErr := w.store.MarkFailed(ctx, job.ID, err.Error()); mfErr != nil {
				w.log.ErrorContext(ctx, "worker: mark failed", slog.String("error", mfErr.Error()))
			}
		} else {
			if mrErr := w.store.MarkRetry(ctx, job.ID, err.Error()); mrErr != nil {
				w.log.ErrorContext(ctx, "worker: mark retry", slog.String("error", mrErr.Error()))
			}
		}
		return
	}

	resultKeyID := ""
	if result.RequestedKey != nil {
		resultKeyID = result.RequestedKey.ID
	}

	if mdErr := w.store.MarkDone(ctx, job.ID, resultKeyID); mdErr != nil {
		w.log.ErrorContext(ctx, "worker: mark done", slog.String("error", mdErr.Error()))
	}

	w.log.InfoContext(ctx, "worker: job done",
		slog.String("jobID", job.ID),
		slog.String("resultKeyID", resultKeyID))

	// Register tenant+system in CMK Registry after key creation.
	// Non-fatal: registry failure is logged but does not affect the job result.
	if resultKeyID != "" {
		regReq := registryclient.EnsureSystemRequest{
			TenantID:   job.TenantID,
			TenantName: job.TenantID,
			ServiceID:  job.ServiceID,
			L2KeyID:    resultKeyID,
			Region:     w.region,
			OwnerID:    w.ownerID,
			OwnerType:  w.ownerType,
			SystemType: w.systemType,
			Role:       w.role,
		}
		if regErr := w.registry.EnsureSystem(ctx, regReq); regErr != nil {
			w.log.ErrorContext(ctx, "worker: registry EnsureSystem failed — key created but not registered",
				slog.String("jobID", job.ID),
				slog.String("tenantID", job.TenantID),
				slog.String("serviceID", job.ServiceID),
				slog.String("error", regErr.Error()))
		} else {
			w.log.InfoContext(ctx, "worker: registered in CMK Registry",
				slog.String("tenantID", job.TenantID),
				slog.String("serviceID", job.ServiceID))
		}
	}
}

// Pool manages a fixed number of concurrent workers plus a stuck-job recovery routine.
type Pool struct {
	workers      int
	store        jobstore.JobStore
	orch         *orchestrator.Orchestrator
	registry     registryclient.Client
	log          *slog.Logger
	pollInterval time.Duration
	stuckTimeout time.Duration
	region       string
	ownerID      string
	ownerType    string
	systemType   string
	role         string
}

func NewPool(
	workers int,
	store jobstore.JobStore,
	orch *orchestrator.Orchestrator,
	registry registryclient.Client,
	log *slog.Logger,
	pollInterval time.Duration,
	stuckTimeout time.Duration,
	region string,
	ownerID string,
	ownerType string,
	systemType string,
	role string,
) *Pool {
	return &Pool{
		workers:      workers,
		store:        store,
		orch:         orch,
		registry:     registry,
		log:          log,
		pollInterval: pollInterval,
		stuckTimeout: stuckTimeout,
		region:       region,
		ownerID:      ownerID,
		ownerType:    ownerType,
		systemType:   systemType,
		role:         role,
	}
}

// Start launches worker goroutines and the stuck-job recovery loop.
// Blocks until ctx is cancelled.
func (p *Pool) Start(ctx context.Context) {
	var wg sync.WaitGroup

	for i := range p.workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w := &Worker{
				store:        p.store,
				orch:         p.orch,
				registry:     p.registry,
				log:          p.log.With(slog.Int("workerID", id)),
				stuckTimeout: p.stuckTimeout,
				region:       p.region,
				ownerID:      p.ownerID,
				ownerType:    p.ownerType,
				systemType:   p.systemType,
				role:         p.role,
			}
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
	wg.Go(func() {
		ticker := time.NewTicker(p.stuckTimeout / 2)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := p.store.RecoverStuck(ctx, p.stuckTimeout)
				if err != nil {
					p.log.ErrorContext(ctx, "worker: recover stuck jobs", slog.String("error", err.Error()))
				} else if n > 0 {
					p.log.InfoContext(ctx, "worker: recovered stuck jobs", slog.Int("count", n))
				}
			}
		}
	})

	wg.Wait()
}
