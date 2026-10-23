package kryptonclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"regexp"

	"github.com/openkcm/common-sdk/pkg/commoncfg"
	"github.com/openkcm/common-sdk/pkg/commongrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	adminpb "github.com/openkcm/cmk-krypton-bridge/internal/gen/krypton/admin"
	kryptonpb "github.com/openkcm/cmk-krypton-bridge/internal/gen/krypton/admin/keys"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// GRPCClient is the real Krypton client backed by krypton's KeyService gRPC API.
type GRPCClient struct {
	stub       kryptonpb.KeyServiceClient
	tenantStub adminpb.TenantServiceClient
}

// NewGRPCClient dials Krypton and returns a ready client.
// Transport security is derived from cfg.SecretRef:
//
//	insecure → plaintext, tls → system-CA TLS, mtls → mutual TLS.
func NewGRPCClient(cfg commoncfg.GRPCClient) (*GRPCClient, error) {
	var conn *grpc.ClientConn
	var err error

	if cfg.SecretRef != nil && cfg.SecretRef.Type == "tls" {
		// Envoy Gateway does not advertise h2 ALPN — use a permissive TLS credential
		// that skips the ALPN check enforced by grpc-go >= 1.67.
		tlsCreds := newPermissiveTLS()
		conn, err = grpc.NewClient(cfg.Address, grpc.WithTransportCredentials(tlsCreds))
	} else {
		conn, err = commongrpc.NewClient(&cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to krypton at %s: %w", cfg.Address, err)
	}
	return &GRPCClient{
		stub:       kryptonpb.NewKeyServiceClient(conn),
		tenantStub: adminpb.NewTenantServiceClient(conn),
	}, nil
}

// GetKey fetches a key by ID and tenant from Krypton.
// tenantID may be a name or UUID; if not a UUID, resolves via EnsureTenant.
// Tries direct ID lookup first; falls back to name-based search for our deterministic key names.
func (c *GRPCClient) GetKey(ctx context.Context, id, tenantID string) (*Key, error) {
	tenantUUID, err := c.EnsureTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("resolving tenant: %w", err)
	}
	resp, err := c.stub.GetKey(ctx, &kryptonpb.GetKeyRequest{
		Id:       id,
		TenantId: tenantUUID,
	})
	if err == nil {
		return protoToKey(resp.GetKey()), nil
	}

	// Krypton returns Internal with "failed to get key" or NotFound when key doesn't exist.
	// Fall through to name-based search using our deterministic key names.
	if status.Code(err) == codes.Internal {
		msg := status.Convert(err).Message()
		if msg != "failed to get key" && msg != "key not found" {
			return nil, fmt.Errorf("krypton GetKey(%s): %w", id, err)
		}
	} else if status.Code(err) != codes.NotFound {
		return nil, fmt.Errorf("krypton GetKey(%s): %w", id, err)
	}

	// Try finding by name: our IDs are formatted as "tenantUUID-l2", etc.
	// Use the full ID as the name hint.
	listResp, listErr := c.stub.ListKeys(ctx, &kryptonpb.ListKeysRequest{
		TenantId: tenantUUID,
		Name:     id,
	})
	if listErr != nil || len(listResp.GetKeys()) == 0 {
		return nil, ErrKeyNotFound
	}
	return protoToKey(listResp.GetKeys()[0]), nil
}

// EnsureKey announces and activates a key (create-if-not-exists).
// Krypton's AnnounceKey is idempotent for the same (tenant, name, kind, parent) tuple.
// The key's Name is set to req.ID (our deterministic name) for later lookup.
func (c *GRPCClient) EnsureKey(ctx context.Context, req EnsureKeyRequest) (*Key, error) {
	// Use req.ID as the key name so we can find it later by name.
	// If ID is empty, fall back to req.Name.
	name := req.ID
	if name == "" {
		name = req.Name
	}

	announceResp, err := c.stub.AnnounceKey(ctx, &kryptonpb.AnnounceKeyRequest{
		TenantId: req.TenantID,
		Kind:     req.Kind,
		Name:     name,
		ParentId: req.ParentID,
		Labels:   req.Metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("krypton AnnounceKey(%s): %w", name, err)
	}

	key := announceResp.GetKey()
	if key.GetLifeCycleState() == "active" {
		return protoToKey(key), nil
	}

	_, err = c.stub.ActivateKey(ctx, &kryptonpb.ActivateKeyRequest{
		Id:       key.GetId(),
		TenantId: req.TenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("krypton ActivateKey(%s): %w", key.GetId(), err)
	}

	// Re-fetch to get the updated lifecycle state
	return c.GetKey(ctx, key.GetId(), req.TenantID)
}

// ListKeysByTenant lists all keys for a tenant.
func (c *GRPCClient) ListKeysByTenant(ctx context.Context, tenantID string) ([]*Key, error) {
	tenantUUID, err := c.EnsureTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("resolving tenant: %w", err)
	}
	resp, err := c.stub.ListKeys(ctx, &kryptonpb.ListKeysRequest{
		TenantId: tenantUUID,
	})
	if err != nil {
		// Krypton returns NotFound when the tenant has no keys yet.
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("krypton ListKeys(tenant=%s): %w", tenantID, err)
	}
	keys := make([]*Key, 0, len(resp.GetKeys()))
	for _, k := range resp.GetKeys() {
		keys = append(keys, protoToKey(k))
	}
	return keys, nil
}

// Health performs a lightweight check by listing tenants.
func (c *GRPCClient) Health(ctx context.Context) error {
	_, err := c.tenantStub.ListTenants(ctx, &adminpb.ListTenantsRequest{})
	if err != nil {
		return fmt.Errorf("krypton health check: %w", err)
	}
	return nil
}

func protoToKey(k *kryptonpb.Key) *Key {
	if k == nil {
		return nil
	}
	return &Key{
		ID:       k.GetId(),
		Name:     k.GetName(),
		TenantID: k.GetTenantId(),
		ParentID: k.GetParentId(),
		Kind:     k.GetKind(),
		Level:    KindToLevel(k.GetKind()),
		Status:   KeyStatus(k.GetLifeCycleState()),
		Metadata: k.GetLabels(),
	}
}

// EnsureTenant returns the UUID for a named tenant, creating it if it doesn't exist.
// If tenantName is already a UUID it is returned as-is (idempotent re-entry).
func (c *GRPCClient) EnsureTenant(ctx context.Context, tenantName string) (string, error) {
	// Already a UUID — pass through directly.
	if uuidRE.MatchString(tenantName) {
		return tenantName, nil
	}

	// List all tenants and find by name.
	listResp, err := c.tenantStub.ListTenants(ctx, &adminpb.ListTenantsRequest{})
	if err != nil {
		return "", fmt.Errorf("krypton ListTenants: %w", err)
	}
	for _, t := range listResp.GetTenants() {
		if t.GetName() == tenantName {
			return t.GetId(), nil
		}
	}

	// Not found — create it.
	createResp, err := c.tenantStub.CreateTenant(ctx, &adminpb.CreateTenantRequest{
		Name: tenantName,
	})
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			// Another pod created the same tenant concurrently — re-list to find it.
			listResp, lerr := c.tenantStub.ListTenants(ctx, &adminpb.ListTenantsRequest{})
			if lerr != nil {
				return "", fmt.Errorf("krypton ListTenants after AlreadyExists: %w", lerr)
			}
			for _, t := range listResp.GetTenants() {
				if t.GetName() == tenantName {
					return t.GetId(), nil
				}
			}
			return "", fmt.Errorf("krypton CreateTenant(%s): AlreadyExists but tenant not found in list", tenantName)
		}
		return "", fmt.Errorf("krypton CreateTenant(%s): %w", tenantName, err)
	}
	return createResp.GetTenant().GetId(), nil
}

var _ Client = (*GRPCClient)(nil)

// permissiveTLS wraps standard TLS credentials but skips the ALPN check
// that grpc-go >= 1.67 enforces. Required when the server (e.g. Envoy Gateway)
// does not advertise h2 in its TLS ALPN extension.
type permissiveTLS struct {
	base credentials.TransportCredentials
}

func newPermissiveTLS() credentials.TransportCredentials {
	return &permissiveTLS{
		base: credentials.NewTLS(&tls.Config{NextProtos: []string{"h2"}}),
	}
}

func (p *permissiveTLS) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := p.base.ClientHandshake(ctx, authority, rawConn)
	if err != nil {
		// Retry without ALPN enforcement: wrap raw conn in TLS ourselves.
		tlsConn := tls.Client(rawConn, &tls.Config{
			ServerName: authority,
			NextProtos: []string{"h2"},
		})
		if hsErr := tlsConn.HandshakeContext(ctx); hsErr != nil {
			return nil, nil, hsErr
		}
		return tlsConn, credentials.TLSInfo{State: tlsConn.ConnectionState()}, nil
	}
	return conn, info, nil
}

func (p *permissiveTLS) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return p.base.ServerHandshake(conn)
}
func (p *permissiveTLS) Info() credentials.ProtocolInfo { return p.base.Info() }
func (p *permissiveTLS) Clone() credentials.TransportCredentials {
	return &permissiveTLS{base: p.base.Clone()}
}
func (p *permissiveTLS) OverrideServerName(name string) error {
	return p.base.OverrideServerName(name) //nolint:staticcheck // Required by TransportCredentials; forwarding preserves override behavior.
}
