//go:build integration

package integration_test

import (
	"context"
	"net"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	rpcv1 "github.com/openkcm/cmk-krypton-bridge/internal/gen/kms/api/krypton/rpc/v1"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
	"github.com/openkcm/cmk-krypton-bridge/internal/server"
)

// TestIntegration_EnsureHierarchy_FullStack tests the complete stack:
// gRPC client → Handler → Orchestrator → MockClient
func TestIntegration_EnsureHierarchy_FullStack(t *testing.T) {
	log := zaptest.NewLogger(t)
	mock := kryptonclient.NewMockClient()
	orch := orchestrator.New(mock, log)
	handler := server.NewHandler(orch, mock, log)

	// Find a free port
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	lis.Close()

	grpcSrv, err := server.NewGRPCServer(handler, addr, log)
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

	// Request L3 → triggers L2 creation
	resp, err := client.EnsureKeyHierarchy(ctx, &rpcv1.EnsureKeyHierarchyRequest{
		TenantId:    "integration-tenant",
		ServiceId:   "orders",
		TargetLevel: rpcv1.KeyLevel_KEY_LEVEL_L3,
	})
	if err != nil {
		t.Fatalf("EnsureKeyHierarchy: %v", err)
	}
	if len(resp.CreatedKeys) != 2 {
		t.Errorf("expected 2 created keys (L2+L3), got %d", len(resp.CreatedKeys))
	}

	// Idempotency: second call must create no new keys
	resp2, err := client.EnsureKeyHierarchy(ctx, &rpcv1.EnsureKeyHierarchyRequest{
		TenantId:    "integration-tenant",
		ServiceId:   "orders",
		TargetLevel: rpcv1.KeyLevel_KEY_LEVEL_L3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.CreatedKeys) != 0 {
		t.Errorf("idempotency violation: expected 0 created keys, got %d", len(resp2.CreatedKeys))
	}
	if resp.RequestedKey.Id != resp2.RequestedKey.Id {
		t.Error("idempotency violation: key ID changed between calls")
	}

	// Cancel context and wait for server goroutine to finish before test teardown.
	cancel()
	<-serveDone
}
