package registryclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	typespb "github.com/openkcm/api-sdk/proto/kms/api/cmk/types/v1"
	mappingpb "github.com/openkcm/api-sdk/proto/kms/api/cmk/registry/mapping/v1"
	systempb "github.com/openkcm/api-sdk/proto/kms/api/cmk/registry/system/v1"
	tenantpb "github.com/openkcm/api-sdk/proto/kms/api/cmk/registry/tenant/v1"
)

// GRPCClientOptions configures the registry gRPC connection.
type GRPCClientOptions struct {
	// BearerToken is sent as "Authorization: Bearer <token>" on every call.
	BearerToken string
	// CACert is the path to a PEM CA certificate used to verify the server.
	// Leave empty to use system roots.
	CACert string
	// ClientCert and ClientKey are paths to PEM files for mTLS.
	// Both must be set together; leave empty for one-way TLS.
	ClientCert string
	ClientKey  string
	// Insecure disables TLS entirely (only for local dev).
	Insecure bool
}

// GRPCClient implements Client against a real Registry gRPC endpoint.
type GRPCClient struct {
	tenantClient  tenantpb.ServiceClient
	systemClient  systempb.ServiceClient
	mappingClient mappingpb.ServiceClient
	bearerToken   string
	log           *slog.Logger
}

// NewGRPCClient dials the Registry with default options (system TLS, no auth).
func NewGRPCClient(address string, log *slog.Logger) (*GRPCClient, error) {
	return NewGRPCClientWithOptions(address, GRPCClientOptions{}, log)
}

// NewGRPCClientWithOptions dials the Registry with explicit TLS/auth options.
func NewGRPCClientWithOptions(address string, opts GRPCClientOptions, log *slog.Logger) (*GRPCClient, error) {
	dialOpts, err := buildDialOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("build registry dial options: %w", err)
	}

	conn, err := grpc.NewClient(address, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial registry %s: %w", address, err)
	}
	return &GRPCClient{
		tenantClient:  tenantpb.NewServiceClient(conn),
		systemClient:  systempb.NewServiceClient(conn),
		mappingClient: mappingpb.NewServiceClient(conn),
		bearerToken:   opts.BearerToken,
		log:           log,
	}, nil
}

func buildDialOptions(opts GRPCClientOptions) ([]grpc.DialOption, error) {
	if opts.Insecure {
		return []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, nil
	}

	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Custom CA cert.
	if opts.CACert != "" {
		pemData, err := os.ReadFile(opts.CACert)
		if err != nil {
			return nil, fmt.Errorf("read CA cert %s: %w", opts.CACert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("parse CA cert %s: no valid certs found", opts.CACert)
		}
		tlsCfg.RootCAs = pool
	}

	// mTLS: client cert + key.
	if opts.ClientCert != "" || opts.ClientKey != "" {
		if opts.ClientCert == "" || opts.ClientKey == "" {
			return nil, fmt.Errorf("mTLS requires both clientCert and clientKey")
		}
		cert, err := tls.LoadX509KeyPair(opts.ClientCert, opts.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))}, nil
}

// outgoingCtx injects Bearer token into outgoing gRPC metadata when configured.
func (c *GRPCClient) outgoingCtx(ctx context.Context) context.Context {
	if c.bearerToken == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.bearerToken)
}

// EnsureSystem registers the tenant, system, and mapping idempotently.
// AlreadyExists is treated as success for all three steps.
func (c *GRPCClient) EnsureSystem(ctx context.Context, req EnsureSystemRequest) error {
	ctx = c.outgoingCtx(ctx)
	if err := c.ensureTenant(ctx, req); err != nil {
		return err
	}
	if err := c.ensureSystem(ctx, req); err != nil {
		return err
	}
	return c.ensureMapping(ctx, req)
}

// isAlreadyExists returns true for both gRPC AlreadyExists and the registry's
// InvalidArgument "must be unique" / "already exists" pattern.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	if status.Code(err) == codes.AlreadyExists {
		return true
	}
	if status.Code(err) == codes.InvalidArgument {
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "must be unique") || strings.Contains(msg, "already exists")
	}
	return false
}

func (c *GRPCClient) ensureTenant(ctx context.Context, req EnsureSystemRequest) error {
	role := tenantpb.Role_ROLE_TRIAL
	if req.Role != "" {
		if v, ok := tenantpb.Role_value[req.Role]; ok {
			role = tenantpb.Role(v)
		}
	}
	_, err := c.tenantClient.RegisterTenant(ctx, &tenantpb.RegisterTenantRequest{
		Id:        req.TenantID,
		Name:      req.TenantName,
		Region:    req.Region,
		OwnerId:   req.OwnerID,
		OwnerType: req.OwnerType,
		Role:      role,
	})
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("registry RegisterTenant(%s): %w", req.TenantID, err)
	}
	if isAlreadyExists(err) {
		c.log.DebugContext(ctx, "registry: tenant already registered", slog.String("tenantID", req.TenantID))
	}
	return nil
}

func (c *GRPCClient) ensureSystem(ctx context.Context, req EnsureSystemRequest) error {
	_, err := c.systemClient.RegisterSystem(ctx, &systempb.RegisterSystemRequest{
		ExternalId: req.ServiceID,
		TenantId:   req.TenantID,
		L2KeyId:    req.L2KeyID,
		Region:     req.Region,
		Status:     typespb.Status_STATUS_AVAILABLE,
	})
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("registry RegisterSystem(%s): %w", req.ServiceID, err)
	}
	if isAlreadyExists(err) {
		c.log.DebugContext(ctx, "registry: system already registered", slog.String("serviceID", req.ServiceID))
	}
	return nil
}

func (c *GRPCClient) ensureMapping(ctx context.Context, req EnsureSystemRequest) error {
	_, err := c.mappingClient.MapSystemToTenant(ctx, &mappingpb.MapSystemToTenantRequest{
		ExternalId: req.ServiceID,
		TenantId:   req.TenantID,
	})
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("registry MapSystemToTenant(%s→%s): %w", req.ServiceID, req.TenantID, err)
	}
	if isAlreadyExists(err) {
		c.log.DebugContext(ctx, "registry: mapping already exists",
			slog.String("serviceID", req.ServiceID),
			slog.String("tenantID", req.TenantID))
	}
	return nil
}

var _ Client = (*GRPCClient)(nil)
