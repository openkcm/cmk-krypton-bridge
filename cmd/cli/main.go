package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	rpcv1 "github.com/openkcm/cmk-krypton-bridge/internal/gen/kms/api/krypton/rpc/v1"
)

var proxyAddr string

func main() {
	root := &cobra.Command{
		Use:   "cmk2kryptonctl",
		Short: "CLI for managing the CmkKryptonBridge",
	}
	root.PersistentFlags().StringVar(&proxyAddr, "addr", "localhost:9092", "CmkKryptonBridge gRPC address")
	root.AddCommand(healthCmd(), listKeysCmd(), ensureHierarchyCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newClient() (rpcv1.CmkKryptonBridgeServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(proxyAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("connecting to %s: %w", proxyAddr, err)
	}
	return rpcv1.NewCmkKryptonBridgeServiceClient(conn), conn, nil
}

func healthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check CmkKryptonBridge health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, conn, err := newClient()
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := client.Health(context.Background(), &rpcv1.HealthRequest{})
			if err != nil {
				return err
			}
			return printProto(resp)
		},
	}
}

func listKeysCmd() *cobra.Command {
	var tenantID string
	cmd := &cobra.Command{
		Use:   "list-keys",
		Short: "List all keys for a tenant",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tenantID == "" {
				return fmt.Errorf("--tenant-id is required")
			}
			client, conn, err := newClient()
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := client.ListKeys(context.Background(), &rpcv1.ListKeysRequest{TenantId: tenantID})
			if err != nil {
				return err
			}
			return printProto(resp)
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant ID")
	return cmd
}

func ensureHierarchyCmd() *cobra.Command {
	var tenantID, serviceID, level string
	cmd := &cobra.Command{
		Use:   "ensure-hierarchy",
		Short: "Ensure key hierarchy for a tenant/service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tenantID == "" {
				return fmt.Errorf("--tenant-id is required")
			}
			var lvl rpcv1.KeyLevel
			switch level {
			case "L2":
				lvl = rpcv1.KeyLevel_KEY_LEVEL_L2
			case "L3":
				lvl = rpcv1.KeyLevel_KEY_LEVEL_L3
			case "L4":
				lvl = rpcv1.KeyLevel_KEY_LEVEL_L4
			default:
				return fmt.Errorf("--level must be L2, L3 or L4")
			}
			client, conn, err := newClient()
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := client.EnsureKeyHierarchy(context.Background(), &rpcv1.EnsureKeyHierarchyRequest{
				TenantId:    tenantID,
				ServiceId:   serviceID,
				TargetLevel: lvl,
			})
			if err != nil {
				return err
			}
			return printProto(resp)
		},
	}
	cmd.Flags().StringVar(&tenantID, "tenant-id", "", "Tenant ID (required)")
	cmd.Flags().StringVar(&serviceID, "service-id", "", "Service ID (required for L3/L4)")
	cmd.Flags().StringVar(&level, "level", "L3", "Target key level: L2, L3, L4")
	return cmd
}

var protoMarshaler = protojson.MarshalOptions{
	Multiline:     true,
	Indent:        "  ",
	UseProtoNames: true,
}

func printProto(m proto.Message) error {
	b, err := protoMarshaler.Marshal(m)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
