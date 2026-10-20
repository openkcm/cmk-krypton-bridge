package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

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
	root.AddCommand(healthCmd(), listKeysCmd(), ensureHierarchyCmd(), getJobStatusCmd(), watchJobCmd())

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
				return errors.New("--tenant-id is required")
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
		Short: "Enqueue a key hierarchy job for a tenant/service",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if tenantID == "" {
				return errors.New("--tenant-id is required")
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
				return errors.New("--level must be L2, L3 or L4")
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

func getJobStatusCmd() *cobra.Command {
	var jobID string
	cmd := &cobra.Command{
		Use:   "get-job-status",
		Short: "Poll the status of a key hierarchy job",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jobID == "" {
				return errors.New("--job-id is required")
			}
			client, conn, err := newClient()
			if err != nil {
				return err
			}
			defer conn.Close()
			resp, err := client.GetJobStatus(context.Background(), &rpcv1.GetJobStatusRequest{JobId: jobID})
			if err != nil {
				return err
			}
			return printProto(resp)
		},
	}
	cmd.Flags().StringVar(&jobID, "job-id", "", "Job ID returned by ensure-hierarchy")
	return cmd
}

func watchJobCmd() *cobra.Command {
	var jobID string
	var intervalSec int
	var timeoutSec int
	cmd := &cobra.Command{
		Use:   "watch-job",
		Short: "Poll a job until it reaches DONE or FAILED",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if jobID == "" {
				return errors.New("--job-id is required")
			}
			client, conn, err := newClient()
			if err != nil {
				return err
			}
			defer conn.Close()

			deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
			interval := time.Duration(intervalSec) * time.Second

			for {
				resp, err := client.GetJobStatus(context.Background(), &rpcv1.GetJobStatusRequest{JobId: jobID})
				if err != nil {
					return err
				}

				fmt.Printf("[%s] status: %s  attempts: %d\n",
					time.Now().Format("15:04:05"), resp.GetStatus(), resp.GetAttempts())

				switch resp.GetStatus() {
				case rpcv1.JobStatus_JOB_STATUS_DONE:
					fmt.Println()
					return printProto(resp)
				case rpcv1.JobStatus_JOB_STATUS_FAILED:
					fmt.Println()
					printProto(resp) //nolint:errcheck
					return fmt.Errorf("job failed: %s", resp.GetError())
				}

				if time.Now().After(deadline) {
					return fmt.Errorf("timeout after %ds — last status: %s", timeoutSec, resp.GetStatus())
				}
				time.Sleep(interval)
			}
		},
	}
	cmd.Flags().StringVar(&jobID, "job-id", "", "Job ID to watch")
	cmd.Flags().IntVar(&intervalSec, "interval", 2, "Poll interval in seconds")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 60, "Timeout in seconds")
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
