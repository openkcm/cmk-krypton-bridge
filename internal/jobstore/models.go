package jobstore

import "time"

// Status represents the lifecycle state of a key hierarchy job.
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDone       Status = "done"
	StatusRetrying   Status = "retrying"
	StatusFailed     Status = "failed"
)

// Job holds a single enqueued EnsureKeyHierarchy request and its current state.
type Job struct {
	ID          string
	Status      Status
	TenantID    string
	ServiceID   string
	Level       int
	Metadata    map[string]string
	Attempts    int
	MaxAttempts int
	LastError   string
	ResultKeyID string
	RetryAfter  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
