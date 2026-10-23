package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
)

// Level-to-Krypton-kind mapping.
// The running Krypton instance uses K0/K1/K2 as configured in its hierarchy:
//
//	K0 = root key (no parent) → our L2 (tenant root)
//	K1 = KEK (parent=K0)     → our L3 (service key)
//	K2 = DEK (parent=K1)     → our L4
const (
	KindL2 = "K0" // tenant root (root kind, no parent)
	KindL3 = "K1" // service key (KEK, parent=K0)
	KindL4 = "K2" // DEK, parent=K1

	globalL1KeyID = "l1-global"
)

// EnsureHierarchyRequest describes which key is needed for a tenant/service.
type EnsureHierarchyRequest struct {
	TenantID  string
	ServiceID string
	Level     kryptonclient.Level
	Metadata  map[string]string
}

// EnsureHierarchyResult contains all keys that were ensured/created.
type EnsureHierarchyResult struct {
	RequestedKey *kryptonclient.Key
	CreatedKeys  []*kryptonclient.Key
	EnsuredKeys  []*kryptonclient.Key
}

// Orchestrator manages the key hierarchy.
type Orchestrator struct {
	client kryptonclient.Client
	log    *slog.Logger
	l1ID   string
	// skipL1Lookup disables fetching the L1 key from the backend.
	// Set true when the backend manages its own hardware root (real Krypton).
	skipL1Lookup bool
}

// New creates a new Orchestrator.
func New(client kryptonclient.Client, log *slog.Logger) *Orchestrator {
	return &Orchestrator{
		client: client,
		log:    log,
		l1ID:   globalL1KeyID,
	}
}

// NewWithOptions creates an Orchestrator with additional options.
func NewWithOptions(client kryptonclient.Client, log *slog.Logger, skipL1Lookup bool) *Orchestrator {
	return &Orchestrator{
		client:       client,
		log:          log,
		l1ID:         globalL1KeyID,
		skipL1Lookup: skipL1Lookup,
	}
}

// EnsureHierarchy ensures the requested key (and all parent keys) exist.
// Idempotent: safe to call multiple times.
func (o *Orchestrator) EnsureHierarchy(
	ctx context.Context,
	req EnsureHierarchyRequest,
) (*EnsureHierarchyResult, error) {
	if req.TenantID == "" {
		return nil, errors.New("tenantID is required")
	}
	if req.Level < kryptonclient.LevelL2 || req.Level > kryptonclient.LevelL4 {
		return nil, fmt.Errorf("level must be L2, L3 or L4 (got %d)", req.Level)
	}
	if req.Level >= kryptonclient.LevelL3 && req.ServiceID == "" {
		return nil, errors.New("serviceID is required for L3/L4")
	}

	// Resolve tenant name → UUID (real Krypton requires UUID tenant IDs).
	tenantUUID, err := o.client.EnsureTenant(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("resolving tenant %q: %w", req.TenantID, err)
	}
	if tenantUUID != req.TenantID {
		o.log.InfoContext(ctx, "resolved tenant name to UUID",
			slog.String("tenantName", req.TenantID),
			slog.String("tenantUUID", tenantUUID))
	}

	// Work with UUID internally.
	resolvedReq := req
	resolvedReq.TenantID = tenantUUID

	result := &EnsureHierarchyResult{}
	key, err := o.ensureLevel(ctx, resolvedReq, result)
	if err != nil {
		return nil, err
	}
	result.RequestedKey = key
	return result, nil
}

// ensureLevel recursively creates all necessary parent keys.
func (o *Orchestrator) ensureLevel(
	ctx context.Context,
	req EnsureHierarchyRequest,
	result *EnsureHierarchyResult,
) (*kryptonclient.Key, error) {
	keyName := buildKeyName(req.ServiceID, req.Level)
	keyID := buildKeyID(req.TenantID, req.ServiceID, req.Level)

	existing, err := o.client.GetKey(ctx, keyID, req.TenantID)
	if err == nil {
		existing.ProxyID = keyID
		result.EnsuredKeys = append(result.EnsuredKeys, existing)
		o.log.InfoContext(ctx, "key already exists",
			slog.String("keyID", keyID),
			slog.Int("level", int(req.Level)))
		return existing, nil
	}
	if !errors.Is(err, kryptonclient.ErrKeyNotFound) {
		return nil, fmt.Errorf("checking key %q existence: %w", keyID, err)
	}

	parentKey, err := o.ensureParent(ctx, req, result)
	if err != nil {
		return nil, fmt.Errorf("ensuring parent for level %d: %w", req.Level, err)
	}

	o.log.InfoContext(ctx, "creating key",
		slog.String("keyID", keyID),
		slog.Int("level", int(req.Level)),
		slog.String("parentID", parentKey.ID))

	newKey, err := o.client.EnsureKey(ctx, kryptonclient.EnsureKeyRequest{
		ID:        keyID,
		Name:      keyName,
		Kind:      levelToKind(req.Level),
		TenantID:  req.TenantID,
		ServiceID: req.ServiceID,
		ParentID:  parentKey.ID, // empty string = no explicit parent (real Krypton L2)
		Metadata:  req.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("creating key %q: %w", keyID, err)
	}

	newKey.ProxyID = keyID
	result.EnsuredKeys = append(result.EnsuredKeys, newKey)
	result.CreatedKeys = append(result.CreatedKeys, newKey)
	return newKey, nil
}

// ensureParent ensures the parent key for the requested level.
func (o *Orchestrator) ensureParent(
	ctx context.Context,
	req EnsureHierarchyRequest,
	result *EnsureHierarchyResult,
) (*kryptonclient.Key, error) {
	switch req.Level {
	case kryptonclient.LevelL2:
		if o.skipL1Lookup {
			// Real Krypton manages its own hardware root — announce L2 without explicit parent.
			return &kryptonclient.Key{ID: ""}, nil
		}
		l1, err := o.client.GetKey(ctx, o.l1ID, "")
		if err != nil {
			return nil, fmt.Errorf("L1 key %q not found — cannot create L2: %w", o.l1ID, err)
		}
		return l1, nil

	case kryptonclient.LevelL3:
		return o.ensureLevel(ctx, EnsureHierarchyRequest{
			TenantID: req.TenantID,
			Level:    kryptonclient.LevelL2,
			Metadata: req.Metadata,
		}, result)

	case kryptonclient.LevelL4:
		return o.ensureLevel(ctx, EnsureHierarchyRequest{
			TenantID:  req.TenantID,
			ServiceID: req.ServiceID,
			Level:     kryptonclient.LevelL3,
			Metadata:  req.Metadata,
		}, result)

	default:
		return nil, fmt.Errorf("unexpected level %d", req.Level)
	}
}

// buildKeyName returns a human-readable key name (used as Krypton key name).
func buildKeyName(serviceID string, level kryptonclient.Level) string {
	switch level {
	case kryptonclient.LevelL2:
		return "tenant-root"
	case kryptonclient.LevelL3:
		return serviceID + "-service-key"
	case kryptonclient.LevelL4:
		return serviceID + "-dek"
	default:
		return fmt.Sprintf("level-%d", level)
	}
}

// buildKeyID generates a deterministic key ID for in-proxy lookups.
// Determinism is critical for idempotency: same input → same ID.
func buildKeyID(tenantID, serviceID string, level kryptonclient.Level) string {
	switch level {
	case kryptonclient.LevelL1:
		return globalL1KeyID
	case kryptonclient.LevelL2:
		return tenantID + "-l2"
	case kryptonclient.LevelL3:
		return fmt.Sprintf("%s-%s-l3", tenantID, serviceID)
	case kryptonclient.LevelL4:
		return fmt.Sprintf("%s-%s-l4", tenantID, serviceID)
	default:
		return fmt.Sprintf("unknown-level-%d", level)
	}
}

func levelToKind(level kryptonclient.Level) string {
	switch level {
	case kryptonclient.LevelL2:
		return KindL2
	case kryptonclient.LevelL3:
		return KindL3
	case kryptonclient.LevelL4:
		return KindL4
	default:
		return fmt.Sprintf("level-%d", level)
	}
}
