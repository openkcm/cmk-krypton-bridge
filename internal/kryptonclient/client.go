package kryptonclient

import "context"

// Level represents a tier in the key hierarchy.
type Level int

const (
	LevelL1 Level = 1 // Hardware Root Key (external, read-only in proxy)
	LevelL2 Level = 2 // Tenant Root Key
	LevelL3 Level = 3 // Service Key
	LevelL4 Level = 4 // Data Encryption Key (DEK)
)

// KeyStatus describes the current state of a key.
type KeyStatus string

const (
	KeyStatusActive        KeyStatus = "active"
	KeyStatusPreActivation KeyStatus = "pre-activation"
	KeyStatusDisabled      KeyStatus = "suspended"
	KeyStatusDeleted       KeyStatus = "destroyed"
)

// Key is the internal representation of a key in CmkKryptonBridge.
type Key struct {
	ID string
	// ProxyID is the deterministic identifier used by the bridge. ID remains
	// the identifier returned by the backing key service.
	ProxyID   string
	Name      string
	Level     Level
	TenantID  string
	ServiceID string
	ParentID  string
	Kind      string
	Status    KeyStatus
	Metadata  map[string]string
}

// KindToLevel maps a Krypton kind string to a proxy Level.
// The running Krypton instance uses K0/K1/K2 for its configured hierarchy:
//
//	K0 = root (no parent)    → L2 in our proxy model (tenant root)
//	K1 = KEK (parent=K0)    → L3 in our proxy model (service key)
//	K2 = DEK (parent=K1)    → L4 in our proxy model (DEK)
//
// Also handles legacy kind names for backward compatibility with mock usage.
func KindToLevel(kind string) Level {
	switch kind {
	case "K0", "tenant-root":
		return LevelL2 // tenant root
	case "K1", "service-key":
		return LevelL3 // service key
	case "K2", "dek":
		return LevelL4 // DEK
	case "root":
		return LevelL1 // hardware root (external)
	default:
		return 0
	}
}

// ErrKeyNotFound is returned when a key does not exist.
var ErrKeyNotFound = keyNotFoundError{}

type keyNotFoundError struct{}

func (e keyNotFoundError) Error() string { return "key not found" }
func (e keyNotFoundError) Is(target error) bool {
	_, ok := target.(keyNotFoundError)
	return ok
}

// Client is the interface for all Krypton operations.
// In POC mode it is backed by an in-memory mock; in production by a real gRPC client.
type Client interface {
	// GetKey fetches a key by ID and tenant.
	// Returns ErrKeyNotFound if the key does not exist.
	GetKey(ctx context.Context, id, tenantID string) (*Key, error)

	// EnsureKey announces and activates a key (create-if-not-exists).
	// Maps to Krypton AnnounceKey + ActivateKey.
	// Idempotent: if the key already exists it is returned unchanged.
	EnsureKey(ctx context.Context, req EnsureKeyRequest) (*Key, error)

	// ListKeysByTenant lists all keys for a tenant.
	ListKeysByTenant(ctx context.Context, tenantID string) ([]*Key, error)

	// Health checks whether the Krypton backend is reachable.
	Health(ctx context.Context) error

	// EnsureTenant returns the UUID for a named tenant, creating it if necessary.
	// For mock mode, the tenant name is used as the UUID directly.
	EnsureTenant(ctx context.Context, tenantName string) (uuid string, err error)
}

// EnsureKeyRequest holds all parameters for key creation.
type EnsureKeyRequest struct {
	// ID is an optional hint for the mock client to use as the stored key ID.
	// The real Krypton backend ignores this and returns its own UUID.
	ID        string
	Name      string
	Kind      string
	TenantID  string
	ServiceID string
	ParentID  string
	Metadata  map[string]string
}
