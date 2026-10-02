//go:build integration

package integration_test

import (
	"context"
	"log/slog"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	rpcv1 "github.com/openkcm/cmk-krypton-bridge/internal/gen/kms/api/krypton/rpc/v1"
	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
	"github.com/openkcm/cmk-krypton-bridge/internal/server"
)

// TestIntegration_EnsureHierarchy_FullStack tests the complete stack:
// gRPC client → Handler → JobStore → background worker drives completion.
func TestIntegration_EnsureHierarchy_FullStack(t *testing.T) {
	lg := slog.Default()
	mock := kryptonclient.NewMockClient()
	orch := orchestrator.New(mock, lg)
	store := jobstore.NewInMemoryJobStore()
	handler := server.NewHandler(orch, mock, store, lg)

	// Find a free port.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	lis.Close()

	grpcSrv, err := server.NewGRPCServer(handler, addr, lg)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		grpcSrv.Serve(ctx) //nolint:errcheck
	}()
	time.Sleep(50 * time.Millisecond)

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := rpcv1.NewCmkKryptonBridgeServiceClient(conn)

	// Submit a job — returns job_id + ACCEPTED.
	resp, err := client.EnsureKeyHierarchy(ctx, &rpcv1.EnsureKeyHierarchyRequest{
		TenantId:    "integration-tenant",
		ServiceId:   "orders",
		TargetLevel: rpcv1.KeyLevel_KEY_LEVEL_L3,
	})
	if err != nil {
		t.Fatalf("EnsureKeyHierarchy: %v", err)
	}
	if resp.JobId == "" {
		t.Fatal("expected non-empty job_id")
	}
	if resp.Status != rpcv1.JobStatus_JOB_STATUS_ACCEPTED {
		t.Errorf("expected ACCEPTED, got %v", resp.Status)
	}

	cancel()
	<-serveDone
}
