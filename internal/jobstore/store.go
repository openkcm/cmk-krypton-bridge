package jobstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
)

// JobStore is the persistence interface for key hierarchy jobs.
type JobStore interface {
	// Enqueue writes a new job with status=pending. Returns the job ID.
	Enqueue(ctx context.Context, req orchestrator.EnsureHierarchyRequest) (id string, err error)

	// ClaimNext atomically selects one claimable job (pending/retrying with retry_after <= now)
	// and transitions it to processing. Returns nil, nil if the queue is empty.
	ClaimNext(ctx context.Context) (*Job, error)

	// Heartbeat bumps updated_at on a processing job so RecoverStuck won't reset it.
	Heartbeat(ctx context.Context, id string) error

	// MarkDone sets status=done and stores the result key ID.
	MarkDone(ctx context.Context, id, resultKeyID string) error

	// RecordManagedKeys persists the bridge-to-backend key mappings for a job.
	RecordManagedKeys(ctx context.Context, jobID string, keys []*kryptonclient.Key) error

	// MarkFailed sets status=failed with an error message.
	MarkFailed(ctx context.Context, id, errMsg string) error

	// MarkRetry increments attempts, sets status=retrying, and schedules the next retry
	// using exponential backoff with jitter: base * 2^attempts + rand(0..base).
	MarkRetry(ctx context.Context, id, errMsg string) error

	// GetJob retrieves a job by ID.
	GetJob(ctx context.Context, id string) (*Job, error)

	// RecoverStuck resets jobs stuck in processing longer than threshold back to pending.
	// Returns the number of jobs reset.
	RecoverStuck(ctx context.Context, threshold time.Duration) (int, error)
}

const retryBase = 5 * time.Second

// PostgresJobStore implements JobStore backed by PostgreSQL.
type PostgresJobStore struct {
	db *sql.DB
}

// NewPostgresJobStore creates a new store using the provided *sql.DB.
func NewPostgresJobStore(db *sql.DB) *PostgresJobStore {
	return &PostgresJobStore{db: db}
}

func (s *PostgresJobStore) Enqueue(ctx context.Context, req orchestrator.EnsureHierarchyRequest) (string, error) {
	metaJSON, err := json.Marshal(req.Metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	var id string
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO key_hierarchy_jobs (tenant_id, service_id, level, metadata)
		VALUES ($1, $2, $3, $4)
		RETURNING id`,
		req.TenantID, req.ServiceID, int(req.Level), metaJSON,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("enqueue job: %w", err)
	}
	return id, nil
}

func (s *PostgresJobStore) ClaimNext(ctx context.Context) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE key_hierarchy_jobs
		SET status     = 'processing',
		    updated_at = NOW()
		WHERE id = (
		    SELECT id FROM key_hierarchy_jobs
		    WHERE  status IN ('pending', 'retrying')
		      AND  retry_after <= NOW()
		    ORDER BY retry_after
		    LIMIT  1
		    FOR UPDATE SKIP LOCKED
		)
		RETURNING id, status, tenant_id, service_id, level, metadata,
		          attempts, max_attempts, last_error, result_key_id,
		          retry_after, created_at, updated_at`)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next job: %w", err)
	}
	return job, nil
}

func (s *PostgresJobStore) MarkDone(ctx context.Context, id, resultKeyID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE key_hierarchy_jobs
		SET status = 'done', result_key_id = $2, updated_at = NOW()
		WHERE id = $1`, id, resultKeyID)
	return err
}

func (s *PostgresJobStore) RecordManagedKeys(ctx context.Context, jobID string, keys []*kryptonclient.Key) error {
	for _, key := range keys {
		if key == nil || key.ProxyID == "" {
			return fmt.Errorf("record managed key: missing proxy ID")
		}
		metadataJSON, err := json.Marshal(key.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata for managed key %q: %w", key.ProxyID, err)
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO managed_keys
			    (job_id, proxy_key_id, krypton_key_id, tenant_id, service_id,
			     level, parent_krypton_key_id, name, kind, status, metadata)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (proxy_key_id) DO UPDATE SET
			    job_id = EXCLUDED.job_id,
			    krypton_key_id = EXCLUDED.krypton_key_id,
			    tenant_id = EXCLUDED.tenant_id,
			    service_id = EXCLUDED.service_id,
			    level = EXCLUDED.level,
			    parent_krypton_key_id = EXCLUDED.parent_krypton_key_id,
			    name = EXCLUDED.name,
			    kind = EXCLUDED.kind,
			    status = EXCLUDED.status,
			    metadata = EXCLUDED.metadata,
			    last_seen_at = NOW(),
			    updated_at = NOW()`,
			jobID, key.ProxyID, key.ID, key.TenantID, key.ServiceID,
			int(key.Level), key.ParentID, key.Name, key.Kind, string(key.Status), metadataJSON)
		if err != nil {
			return fmt.Errorf("record managed key %q: %w", key.ProxyID, err)
		}
	}
	return nil
}

func (s *PostgresJobStore) MarkFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE key_hierarchy_jobs
		SET status     = 'failed',
		    last_error = $2,
		    attempts   = attempts + 1,
		    updated_at = NOW()
		WHERE id = $1`, id, errMsg)
	return err
}

func (s *PostgresJobStore) Heartbeat(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE key_hierarchy_jobs SET updated_at = NOW() WHERE id = $1 AND status = 'processing'`, id)
	return err
}

func (s *PostgresJobStore) MarkRetry(ctx context.Context, id, errMsg string) error {
	// Atomically increment attempts and read new value to compute backoff.
	var newAttempts int
	err := s.db.QueryRowContext(ctx, `
		UPDATE key_hierarchy_jobs
		SET attempts   = attempts + 1,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING attempts`, id).Scan(&newAttempts)
	if err != nil {
		return fmt.Errorf("increment attempts: %w", err)
	}

	// Backoff is based on the attempt count before this failure (newAttempts-1).
	prev := newAttempts - 1
	if prev < 0 {
		prev = 0
	}
	backoff := retryBase * (1 << prev)
	jitter := time.Duration(rand.Int63n(int64(retryBase)))
	nextRetry := time.Now().Add(backoff + jitter)

	_, err = s.db.ExecContext(ctx, `
		UPDATE key_hierarchy_jobs
		SET status      = 'retrying',
		    last_error  = $2,
		    retry_after = $3,
		    updated_at  = NOW()
		WHERE id = $1`, id, errMsg, nextRetry)
	return err
}

func (s *PostgresJobStore) GetJob(ctx context.Context, id string) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, status, tenant_id, service_id, level, metadata,
		       attempts, max_attempts, last_error, result_key_id,
		       retry_after, created_at, updated_at
		FROM key_hierarchy_jobs
		WHERE id = $1`, id)

	job, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("job %q not found", id)
	}
	return job, err
}

func (s *PostgresJobStore) RecoverStuck(ctx context.Context, threshold time.Duration) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE key_hierarchy_jobs
		SET status     = 'pending',
		    retry_after = NOW(),
		    updated_at = NOW()
		WHERE status     = 'processing'
		  AND updated_at < NOW() - $1::interval`, threshold.String())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (*Job, error) {
	var j Job
	var metaJSON []byte
	var statusStr string
	err := row.Scan(
		&j.ID, &statusStr, &j.TenantID, &j.ServiceID, &j.Level, &metaJSON,
		&j.Attempts, &j.MaxAttempts, &j.LastError, &j.ResultKeyID,
		&j.RetryAfter, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	j.Status = Status(statusStr)
	if len(metaJSON) > 0 {
		_ = json.Unmarshal(metaJSON, &j.Metadata)
	}
	return &j, nil
}
