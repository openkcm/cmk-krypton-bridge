package eventprocessor

import (
	"context"

	"github.com/openkcm/orbital"
)

// JobType mirrors CMK's JobType — task type strings sent by the orbital Manager.
type JobType string

func (t JobType) String() string {
	return string(t)
}

const (
	JobTypeSystemLink      JobType = "SYSTEM_LINK"
	JobTypeSystemUnlink    JobType = "SYSTEM_UNLINK"
	JobTypeSystemSwitch    JobType = "SYSTEM_SWITCH"
	JobTypeSystemKeyRotate JobType = "SYSTEM_KEY_ROTATE"
	JobTypeKeyEnable       JobType = "KEY_ENABLE"
	JobTypeKeyDisable      JobType = "KEY_DISABLE"
	JobTypeKeyDetach       JobType = "KEY_DETACH"
	JobTypeKeyDelete       JobType = "KEY_DELETE"
)

// JobHandler mirrors CMK's JobHandler interface.
type JobHandler interface {
	HandleJobDoneEvent(ctx context.Context, job orbital.Job) error
	HandleJobFailedEvent(ctx context.Context, job orbital.Job) error
	HandleJobCanceledEvent(ctx context.Context, job orbital.Job) error
}

// KeyActionJobData mirrors CMK's KeyActionJobData (JSON in orbital Job.Data).
type KeyActionJobData struct {
	TenantID string `json:"tenantID"`
	KeyID    string `json:"keyID"`
}

// SystemActionJobData mirrors CMK's SystemActionJobData (JSON in orbital Job.Data).
type SystemActionJobData struct {
	SystemID  string `json:"systemID"`
	TenantID  string `json:"tenantID"`
	KeyIDTo   string `json:"keyIDTo"`
	KeyIDFrom string `json:"keyIDFrom"`
	Trigger   string `json:"trigger,omitempty"`
}
