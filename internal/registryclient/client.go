package registryclient

import "context"

// EnsureSystemRequest holds the data needed to register a system + tenant in the Registry.
type EnsureSystemRequest struct {
	TenantID   string
	TenantName string
	ServiceID  string
	L2KeyID    string
	Region     string
	OwnerID    string
	OwnerType  string
	Role       string // e.g. "ROLE_TRIAL", "ROLE_LIVE" — defaults to ROLE_TRIAL if empty
}

// Client registers tenants and systems in the CMK Registry.
// All operations are idempotent — safe to call on every successful key creation.
type Client interface {
	// EnsureSystem registers the tenant, system, and mapping in the Registry.
	// AlreadyExists responses are treated as success.
	// Returns nil if Registry is disabled (noop mode).
	EnsureSystem(ctx context.Context, req EnsureSystemRequest) error
}
