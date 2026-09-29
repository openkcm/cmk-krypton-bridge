package orchestrator_test

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap/zaptest"

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
)

func TestEnsureHierarchy_L3_L2Missing(t *testing.T) {
	mock := kryptonclient.NewMockClient()
	orch := orchestrator.New(mock, zaptest.NewLogger(t))

	result, err := orch.EnsureHierarchy(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID:  "tenant-abc",
		ServiceID: "payments",
		Level:     kryptonclient.LevelL3,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequestedKey == nil {
		t.Fatal("expected non-nil RequestedKey")
	}
	if result.RequestedKey.Level != kryptonclient.LevelL3 {
		t.Errorf("expected L3, got %d", result.RequestedKey.Level)
	}
	if len(result.CreatedKeys) != 2 {
		t.Errorf("expected 2 created keys (L2+L3), got %d", len(result.CreatedKeys))
	}
}

func TestEnsureHierarchy_L3_L2Exists(t *testing.T) {
	mock := kryptonclient.NewMockClient()
	// Pre-create L2 with the deterministic proxy ID
	_, err := mock.EnsureKey(context.Background(), kryptonclient.EnsureKeyRequest{
		ID:       "tenant-abc-l2",
		Name:     "tenant-root",
		Kind:     "tenant-root",
		TenantID: "tenant-abc",
		ParentID: "l1-global",
	})
	if err != nil {
		t.Fatal(err)
	}

	orch := orchestrator.New(mock, zaptest.NewLogger(t))
	result, err := orch.EnsureHierarchy(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID:  "tenant-abc",
		ServiceID: "payments",
		Level:     kryptonclient.LevelL3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.CreatedKeys) != 1 {
		t.Errorf("expected 1 created key (L3 only), got %d", len(result.CreatedKeys))
	}
}

func TestEnsureHierarchy_Idempotent(t *testing.T) {
	mock := kryptonclient.NewMockClient()
	orch := orchestrator.New(mock, zaptest.NewLogger(t))
	req := orchestrator.EnsureHierarchyRequest{
		TenantID:  "tenant-abc",
		ServiceID: "payments",
		Level:     kryptonclient.LevelL3,
	}

	r1, err := orch.EnsureHierarchy(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	r2, err := orch.EnsureHierarchy(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if r1.RequestedKey.ID != r2.RequestedKey.ID {
		t.Errorf("idempotency violated: got different key IDs %q vs %q",
			r1.RequestedKey.ID, r2.RequestedKey.ID)
	}
	if len(r2.CreatedKeys) != 0 {
		t.Errorf("second call should create no keys, got %d", len(r2.CreatedKeys))
	}
}

func TestEnsureHierarchy_L4_FullChain(t *testing.T) {
	mock := kryptonclient.NewMockClient()
	orch := orchestrator.New(mock, zaptest.NewLogger(t))

	result, err := orch.EnsureHierarchy(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID:  "tenant-xyz",
		ServiceID: "invoicing",
		Level:     kryptonclient.LevelL4,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.CreatedKeys) != 3 {
		t.Errorf("expected 3 created keys (L2+L3+L4), got %d", len(result.CreatedKeys))
	}
	if result.RequestedKey.Level != kryptonclient.LevelL4 {
		t.Errorf("expected L4, got %d", result.RequestedKey.Level)
	}
}

func TestEnsureHierarchy_MissingTenantID(t *testing.T) {
	mock := kryptonclient.NewMockClient()
	orch := orchestrator.New(mock, zaptest.NewLogger(t))
	_, err := orch.EnsureHierarchy(context.Background(), orchestrator.EnsureHierarchyRequest{
		Level: kryptonclient.LevelL3,
	})
	if err == nil {
		t.Fatal("expected error for missing tenantID")
	}
}

func TestErrKeyNotFound_Is(t *testing.T) {
	err := kryptonclient.ErrKeyNotFound
	if !errors.Is(err, kryptonclient.ErrKeyNotFound) {
		t.Error("errors.Is should match ErrKeyNotFound")
	}
}
