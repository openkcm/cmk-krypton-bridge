package server

import (
	"context"
	"errors"
	"net"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	rpcv1 "github.com/openkcm/cmk-krypton-bridge/internal/gen/kms/api/krypton/rpc/v1"
	"github.com/openkcm/cmk-krypton-bridge/internal/jobstore"
	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
)

// Handler implements rpcv1.CmkKryptonBridgeServiceServer.
type Handler struct {
	rpcv1.UnimplementedCmkKryptonBridgeServiceServer
	orch   *orchestrator.Orchestrator
	client kryptonclient.Client
	store  jobstore.JobStore
	log    *zap.Logger
}

// NewHandler creates a new gRPC handler.
func NewHandler(orch *orchestrator.Orchestrator, client kryptonclient.Client, store jobstore.JobStore, log *zap.Logger) *Handler {
	return &Handler{orch: orch, client: client, store: store, log: log}
}

// EnsureKeyHierarchy persists the request as a job and returns immediately with a job_id.
// The background worker pool drives the request to completion asynchronously.
func (h *Handler) EnsureKeyHierarchy(
	ctx context.Context,
	req *rpcv1.EnsureKeyHierarchyRequest,
) (*rpcv1.EnsureKeyHierarchyResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	jobID, err := h.store.Enqueue(ctx, orchestrator.EnsureHierarchyRequest{
		TenantID:  req.TenantId,
		ServiceID: req.ServiceId,
		Level:     kryptonclient.Level(req.TargetLevel),
		Metadata:  req.Metadata,
	})
	if err != nil {
		h.log.Error("enqueue job failed", zap.Error(err), zap.String("tenantID", req.TenantId))
		return nil, status.Errorf(codes.Internal, "enqueue job: %v", err)
	}

	h.log.Info("job enqueued", zap.String("jobID", jobID), zap.String("tenantID", req.TenantId))
	return &rpcv1.EnsureKeyHierarchyResponse{
		JobId:  jobID,
		Status: rpcv1.JobStatus_JOB_STATUS_ACCEPTED,
	}, nil
}

// GetJobStatus returns the current state of a key hierarchy job.
func (h *Handler) GetJobStatus(
	ctx context.Context,
	req *rpcv1.GetJobStatusRequest,
) (*rpcv1.GetJobStatusResponse, error) {
	if req.JobId == "" {
		return nil, status.Error(codes.InvalidArgument, "job_id is required")
	}

	job, err := h.store.GetJob(ctx, req.JobId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "job %q not found", req.JobId)
	}

	resp := &rpcv1.GetJobStatusResponse{
		JobId:    job.ID,
		Status:   jobStatusToProto(job.Status),
		Error:    job.LastError,
		Attempts: int32(job.Attempts),
	}

	if job.Status == jobstore.StatusDone && job.ResultKeyID != "" {
		k, err := h.client.GetKey(ctx, job.ResultKeyID, job.TenantID)
		if err == nil {
			resp.ResultKey = keyToProto(k)
		}
	}

	return resp, nil
}

func (h *Handler) GetKey(
	ctx context.Context,
	req *rpcv1.GetKeyRequest,
) (*rpcv1.GetKeyResponse, error) {
	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}
	k, err := h.client.GetKey(ctx, req.KeyId, req.TenantId)
	if err != nil {
		if errors.Is(err, kryptonclient.ErrKeyNotFound) {
			return nil, status.Errorf(codes.NotFound, "key %q not found", req.KeyId)
		}
		return nil, status.Errorf(codes.Internal, "get key: %v", err)
	}
	return &rpcv1.GetKeyResponse{Key: keyToProto(k)}, nil
}

func (h *Handler) ListKeys(
	ctx context.Context,
	req *rpcv1.ListKeysRequest,
) (*rpcv1.ListKeysResponse, error) {
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	keys, err := h.client.ListKeysByTenant(ctx, req.TenantId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list keys: %v", err)
	}
	resp := &rpcv1.ListKeysResponse{}
	for _, k := range keys {
		resp.Keys = append(resp.Keys, keyToProto(k))
	}
	return resp, nil
}

func (h *Handler) Health(
	ctx context.Context,
	_ *rpcv1.HealthRequest,
) (*rpcv1.HealthResponse, error) {
	kryptonStatus := "ok"
	healthy := true
	if err := h.client.Health(ctx); err != nil {
		kryptonStatus = err.Error()
		healthy = false
	}
	return &rpcv1.HealthResponse{
		Healthy:       healthy,
		KryptonStatus: kryptonStatus,
		Version:       "0.1.0-poc",
	}, nil
}

func keyToProto(k *kryptonclient.Key) *rpcv1.KeyInfo {
	if k == nil {
		return nil
	}
	return &rpcv1.KeyInfo{
		Id:        k.ID,
		Level:     rpcv1.KeyLevel(k.Level),
		TenantId:  k.TenantID,
		ServiceId: k.ServiceID,
		ParentId:  k.ParentID,
		Status:    keyStatusToProto(k.Status),
		Metadata:  k.Metadata,
	}
}

func keyStatusToProto(s kryptonclient.KeyStatus) rpcv1.KeyStatus {
	switch s {
	case kryptonclient.KeyStatusActive:
		return rpcv1.KeyStatus_KEY_STATUS_ACTIVE
	case kryptonclient.KeyStatusDisabled:
		return rpcv1.KeyStatus_KEY_STATUS_DISABLED
	case kryptonclient.KeyStatusDeleted:
		return rpcv1.KeyStatus_KEY_STATUS_DELETED
	default:
		return rpcv1.KeyStatus_KEY_STATUS_UNSPECIFIED
	}
}

func jobStatusToProto(s jobstore.Status) rpcv1.JobStatus {
	switch s {
	case jobstore.StatusPending:
		return rpcv1.JobStatus_JOB_STATUS_ACCEPTED
	case jobstore.StatusProcessing:
		return rpcv1.JobStatus_JOB_STATUS_PROCESSING
	case jobstore.StatusDone:
		return rpcv1.JobStatus_JOB_STATUS_DONE
	case jobstore.StatusRetrying:
		return rpcv1.JobStatus_JOB_STATUS_RETRYING
	case jobstore.StatusFailed:
		return rpcv1.JobStatus_JOB_STATUS_FAILED
	default:
		return rpcv1.JobStatus_JOB_STATUS_UNSPECIFIED
	}
}

// GRPCServer holds the running gRPC server.
type GRPCServer struct {
	srv *grpc.Server
	lis net.Listener
	log *zap.Logger
}

// NewGRPCServer creates and registers the gRPC server.
func NewGRPCServer(handler *Handler, address string, log *zap.Logger) (*GRPCServer, error) {
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	srv := grpc.NewServer()
	rpcv1.RegisterCmkKryptonBridgeServiceServer(srv, handler)
	return &GRPCServer{srv: srv, lis: lis, log: log}, nil
}

// Serve starts the server and blocks until ctx is done.
func (s *GRPCServer) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.log.Info("shutting down gRPC server")
		s.srv.GracefulStop()
	}()
	s.log.Info("gRPC server listening", zap.String("address", s.lis.Addr().String()))
	return s.srv.Serve(s.lis)
}
