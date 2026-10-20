package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	adminpb "github.com/openkcm/cmk-krypton-bridge/internal/gen/krypton/admin"
	keys "github.com/openkcm/cmk-krypton-bridge/internal/gen/krypton/admin/keys"
)

type permissiveTLS struct {
	base credentials.TransportCredentials
}

func (p *permissiveTLS) ClientHandshake(ctx context.Context, authority string, rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := p.base.ClientHandshake(ctx, authority, rawConn)
	if err != nil {
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: authority, NextProtos: []string{"h2"}})
		if hsErr := tlsConn.HandshakeContext(ctx); hsErr != nil {
			return nil, nil, hsErr
		}
		return tlsConn, credentials.TLSInfo{State: tlsConn.ConnectionState()}, nil
	}
	return conn, info, nil
}

func (p *permissiveTLS) ServerHandshake(c net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return p.base.ServerHandshake(c)
}
func (p *permissiveTLS) Info() credentials.ProtocolInfo { return p.base.Info() }
func (p *permissiveTLS) Clone() credentials.TransportCredentials {
	return &permissiveTLS{base: p.base.Clone()}
}

// OverrideServerName is deprecated, but still part of credentials.TransportCredentials.
func (p *permissiveTLS) OverrideServerName(n string) error {
	return p.base.OverrideServerName(n) //nolint:staticcheck // delegates the interface method
}

func main() {
	addr := "ing.worker01.kms.shoot.gardener.cc-one.showroom.apeirora.eu:443"

	creds := &permissiveTLS{base: credentials.NewTLS(&tls.Config{NextProtos: []string{"h2"}})}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		fmt.Printf("dial: %v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	tenantStub := adminpb.NewTenantServiceClient(conn)
	keyStub := keys.NewKeyServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 1: List existing tenants
	fmt.Println("=== ListTenants ===")
	listTenantsResp, err := tenantStub.ListTenants(ctx, &adminpb.ListTenantsRequest{})
	if err != nil {
		fmt.Printf("ListTenants error: %v\n", err)
	} else {
		fmt.Printf("Existing tenants: %d\n", len(listTenantsResp.GetTenants()))
		for _, t := range listTenantsResp.GetTenants() {
			fmt.Printf("  id=%s name=%s\n", t.GetId(), t.GetName())
		}
	}

	// Step 2: Create or get tenant "acme-poc"
	tenantName := "acme-poc"
	var tenantID string
	for _, t := range listTenantsResp.GetTenants() {
		if t.GetName() == tenantName {
			tenantID = t.GetId()
			fmt.Printf("\nFound existing tenant: id=%s name=%s\n", t.GetId(), t.GetName())
			break
		}
	}
	if tenantID == "" {
		fmt.Printf("\n=== CreateTenant name=%s ===\n", tenantName)
		createResp, err := tenantStub.CreateTenant(ctx, &adminpb.CreateTenantRequest{Name: tenantName})
		if err != nil {
			fmt.Printf("CreateTenant error: %v\n", err)
			os.Exit(1)
		}
		tenantID = createResp.GetTenant().GetId()
		fmt.Printf("Created tenant: id=%s name=%s\n", tenantID, createResp.GetTenant().GetName())
	}

	// Step 3: List keys for tenant
	fmt.Printf("\n=== ListKeys tenant=%s ===\n", tenantID)
	listKeysResp, err := keyStub.ListKeys(ctx, &keys.ListKeysRequest{TenantId: tenantID})
	if err != nil {
		fmt.Printf("ListKeys error: %v\n", err)
	} else {
		fmt.Printf("Existing keys: %d\n", len(listKeysResp.GetKeys()))
		for _, k := range listKeysResp.GetKeys() {
			fmt.Printf("  id=%s name=%-20s kind=%-5s state=%s\n", k.GetId(), k.GetName(), k.GetKind(), k.GetLifeCycleState())
		}
	}

	// Step 4: Announce K1 key (no parent — K1 is root kind in this hierarchy)
	keyName := "acme-poc-l2"
	fmt.Printf("\n=== AnnounceKey name=%s kind=K1 ===\n", keyName)
	announceResp, err := keyStub.AnnounceKey(ctx, &keys.AnnounceKeyRequest{
		TenantId: tenantID,
		Kind:     "K1",
		Name:     keyName,
	})
	if err != nil {
		fmt.Printf("AnnounceKey K1 error: %v\n", err)
		// Try K0
		fmt.Printf("\n=== AnnounceKey name=%s kind=K0 ===\n", keyName)
		announceResp, err = keyStub.AnnounceKey(ctx, &keys.AnnounceKeyRequest{
			TenantId: tenantID,
			Kind:     "K0",
			Name:     keyName,
		})
		if err != nil {
			fmt.Printf("AnnounceKey K0 error: %v\n", err)
			return
		}
	}
	k := announceResp.GetKey()
	fmt.Printf("AnnounceKey ok: id=%s kind=%s state=%s\n", k.GetId(), k.GetKind(), k.GetLifeCycleState())

	if k.GetLifeCycleState() != "active" {
		fmt.Printf("\n=== ActivateKey id=%s ===\n", k.GetId())
		_, err = keyStub.ActivateKey(ctx, &keys.ActivateKeyRequest{Id: k.GetId(), TenantId: tenantID})
		if err != nil {
			fmt.Printf("ActivateKey error: %v\n", err)
		} else {
			fmt.Println("ActivateKey ok")
		}
	}
}
