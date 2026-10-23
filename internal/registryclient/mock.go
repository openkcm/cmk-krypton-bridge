package registryclient

import (
	"context"
	"sync"
)

// MockClient is an in-memory implementation of Client for testing and mock mode.
type MockClient struct {
	mu       sync.RWMutex
	tenants  map[string]string     // tenantID → tenantName
	systems  map[string]string     // serviceID → l2KeyID
	mappings map[string]string     // serviceID → tenantID
	Calls    []EnsureSystemRequest // recorded calls, readable in tests
}

// NewMockClient creates a ready MockClient.
func NewMockClient() *MockClient {
	return &MockClient{
		tenants:  make(map[string]string),
		systems:  make(map[string]string),
		mappings: make(map[string]string),
	}
}

func (m *MockClient) EnsureSystem(ctx context.Context, req EnsureSystemRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tenants[req.TenantID] = req.TenantName
	m.systems[req.ServiceID] = req.L2KeyID
	m.mappings[req.ServiceID] = req.TenantID
	m.Calls = append(m.Calls, req)
	return nil
}

// Registered returns true if the service ID has been registered.
func (m *MockClient) Registered(serviceID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.systems[serviceID]
	return ok
}

// TenantRegistered returns true if the tenant ID has been registered.
func (m *MockClient) TenantRegistered(tenantID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.tenants[tenantID]
	return ok
}

// NoopClient implements Client but does nothing — used when registry.enabled=false.
type NoopClient struct{}

func (NoopClient) EnsureSystem(_ context.Context, _ EnsureSystemRequest) error { return nil }

var _ Client = (*MockClient)(nil)
var _ Client = (NoopClient{})
