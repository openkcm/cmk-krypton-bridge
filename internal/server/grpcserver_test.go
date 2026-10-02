package server_test

import (
	"context"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	rpcv1 "github.com/openkcm/cmk-krypton-bridge/internal/gen/kms/api/krypton/rpc/v1"
	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
	"github.com/openkcm/cmk-krypton-bridge/internal/server"
)

func newHandler(t *testing.T) (*server.Handler, *jobstore.InMemoryJobStore, *kryptonclient.MockClient) {
	t.Helper()
	store := jobstore.NewInMemoryJobStore()
	mock := kryptonclient.NewMockClient()
	orch := orchestrator.NewWithOptions(mock, slog.Default(), true)
	h := server.NewHandler(orch, mock, store, slog.Default())
	return h, store, mock
}

func TestEnsureKeyHierarchy_ReturnsJobID(t *testing.T) {
	h, store, _ := newHandler(t)

	resp, err := h.EnsureKeyHierarchy(context.Background(), &rpcv1.EnsureKeyHierarchyRequest{
		TenantId:    "tenant-a",
		TargetLevel: rpcv1.KeyLevel_KEY_LEVEL_L2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.JobId == "" {
		t.Fatal("expected non-empty job_id")
	}
	if resp.Status != rpcv1.JobStatus_JOB_STATUS_ACCEPTED {
		t.Errorf("expected ACCEPTED, got %v", resp.Status)
	}

	// Verify job is in the store.
	job, err := store.GetJob(context.Background(), resp.JobId)
	if err != nil {
		t.Fatalf("job not found in store: %v", err)
	}
	if job.TenantID != "tenant-a" {
		t.Errorf("expected tenant-a, got %q", job.TenantID)
	}
	if job.Status != jobstore.StatusPending {
		t.Errorf("expected pending, got %q", job.Status)
	}
}

func TestEnsureKeyHierarchy_MissingTenantID(t *testing.T) {
	h, _, _ := newHandler(t)

	_, err := h.EnsureKeyHierarchy(context.Background(), &rpcv1.EnsureKeyHierarchyRequest{
		TargetLevel: rpcv1.KeyLevel_KEY_LEVEL_L2,
	})
	if err == nil {
		t.Fatal("expected error for missing tenant_id")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", status.Code(err))
	}
}

func TestGetJobStatus_Pending(t *testing.T) {
	h, store, _ := newHandler(t)

	id, _ := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: "tenant-a",
		Level:    kryptonclient.LevelL2,
	})

	resp, err := h.GetJobStatus(context.Background(), &rpcv1.GetJobStatusRequest{JobId: id})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != rpcv1.JobStatus_JOB_STATUS_ACCEPTED {
		t.Errorf("expected ACCEPTED for pending job, got %v", resp.Status)
	}
}

func TestGetJobStatus_Done_WithResultKey(t *testing.T) {
	h, store, mock := newHandler(t)

	// Pre-create a key in the mock so GetKey works.
	key, _ := mock.EnsureKey(context.Background(), kryptonclient.EnsureKeyRequest{
		ID:       "tenant-a-l2",
		Name:     "tenant-root",
		Kind:     "K0",
		TenantID: "tenant-a",
	})

	id, _ := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: "tenant-a",
		Level:    kryptonclient.LevelL2,
	})
	store.ClaimNext(context.Background())             //nolint
	store.MarkDone(context.Background(), id, key.ID) //nolint

	resp, err := h.GetJobStatus(context.Background(), &rpcv1.GetJobStatusRequest{JobId: id})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != rpcv1.JobStatus_JOB_STATUS_DONE {
		t.Errorf("expected DONE, got %v", resp.Status)
	}
	if resp.ResultKey == nil {
		t.Fatal("expected non-nil result_key for done job")
	}
	if resp.ResultKey.Id != key.ID {
		t.Errorf("expected key ID %q, got %q", key.ID, resp.ResultKey.Id)
	}
}

func TestGetJobStatus_Failed(t *testing.T) {
	h, store, _ := newHandler(t)

	id, _ := store.Enqueue(context.Background(), orchestrator.EnsureHierarchyRequest{
		TenantID: "tenant-a",
		Level:    kryptonclient.LevelL2,
	})
	store.ClaimNext(context.Background())                              //nolint
	store.MarkFailed(context.Background(), id, "krypton unreachable") //nolint

	resp, err := h.GetJobStatus(context.Background(), &rpcv1.GetJobStatusRequest{JobId: id})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != rpcv1.JobStatus_JOB_STATUS_FAILED {
		t.Errorf("expected FAILED, got %v", resp.Status)
	}
	if resp.Error != "krypton unreachable" {
		t.Errorf("expected error message, got %q", resp.Error)
	}
}

func TestGetJobStatus_NotFound(t *testing.T) {
	h, _, _ := newHandler(t)

	_, err := h.GetJobStatus(context.Background(), &rpcv1.GetJobStatusRequest{JobId: "nonexistent-uuid"})
	if err == nil {
		t.Fatal("expected error for unknown job_id")
	}
	if status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound, got %v", status.Code(err))
	}
}

func TestGetJobStatus_MissingJobID(t *testing.T) {
	h, _, _ := newHandler(t)

	_, err := h.GetJobStatus(context.Background(), &rpcv1.GetJobStatusRequest{})
	if err == nil {
		t.Fatal("expected error for missing job_id")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", status.Code(err))
	}
}
