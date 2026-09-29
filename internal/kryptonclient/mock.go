package kryptonclient

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// MockClient is an in-memory implementation of Client.
// Used for the POC — no real Krypton required.
type MockClient struct {
	mu   sync.RWMutex
	keys map[string]*Key // keyID → Key
}

// NewMockClient creates a MockClient pre-populated with the global L1 key.
func NewMockClient() *MockClient {
	m := &MockClient{keys: make(map[string]*Key)}
	m.keys["l1-global"] = &Key{
		ID:       "l1-global",
		Name:     "l1-global",
		Level:    LevelL1,
		TenantID: "",
		Kind:     "root",
		Status:   KeyStatusActive,
		Metadata: map[string]string{"description": "Hardware Root Key (Mock)"},
	}
	return m
}

func (m *MockClient) GetKey(ctx context.Context, id, tenantID string) (*Key, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	k, ok := m.keys[id]
	if !ok {
		return nil, ErrKeyNotFound
	}
	// L1 has no tenant — skip tenant check
	if k.TenantID != "" && k.TenantID != tenantID {
		return nil, ErrKeyNotFound
	}
	return copyKey(k), nil
}

func (m *MockClient) EnsureKey(ctx context.Context, req EnsureKeyRequest) (*Key, error) {
	// Use explicit ID hint when provided (mock path); otherwise derive from tenant+name.
	id := req.ID
	if id == "" {
		id = fmt.Sprintf("%s-%s", req.TenantID, req.Name)
		if req.TenantID == "" {
			id = req.Name
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.keys[id]; ok {
		return copyKey(existing), nil
	}

	if req.ParentID != "" {
		if _, ok := m.keys[req.ParentID]; !ok {
			return nil, fmt.Errorf("parent key %q not found", req.ParentID)
		}
	}

	k := &Key{
		ID:        id,
		Name:      req.Name,
		TenantID:  req.TenantID,
		ServiceID: req.ServiceID,
		ParentID:  req.ParentID,
		Kind:      req.Kind,
		Level:     KindToLevel(req.Kind),
		Status:    KeyStatusActive,
		Metadata:  req.Metadata,
	}
	m.keys[id] = k
	return copyKey(k), nil
}

func (m *MockClient) ListKeysByTenant(ctx context.Context, tenantID string) ([]*Key, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Key
	for _, k := range m.keys {
		if k.TenantID == tenantID {
			result = append(result, copyKey(k))
		}
	}
	return result, nil
}

func (m *MockClient) Health(ctx context.Context) error {
	return nil
}

// EnsureTenant returns the tenant name as its own "UUID" for mock usage.
func (m *MockClient) EnsureTenant(ctx context.Context, tenantName string) (string, error) {
	return tenantName, nil
}

// Reset clears all keys except the L1 root (useful in tests).
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	l1 := m.keys["l1-global"]
	m.keys = map[string]*Key{"l1-global": l1}
}

// NewKeyID generates a non-deterministic ID (for testing only).
func NewKeyID() string {
	return uuid.NewString()[:8]
}

func copyKey(k *Key) *Key {
	if k == nil {
		return nil
	}
	c := *k
	c.Metadata = make(map[string]string, len(k.Metadata))
	for kk, v := range k.Metadata {
		c.Metadata[kk] = v
	}
	return &c
}

var _ Client = (*MockClient)(nil)
