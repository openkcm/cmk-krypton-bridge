// orbital-demo sends all 8 CMK task types to the Bridge via orbital/AMQP
// and reports pass/fail for each one.
//
// Usage:
//
//	bin/orbital-demo \
//	  --amqp-url=amqp://guest:guest@localhost:5672/ \
//	  --amqp-target=/queues/krypton-bridge.requests \
//	  --amqp-source=/queues/krypton-bridge.replies
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"uuid"

	"google.golang.org/protobuf/proto"

	"github.com/openkcm/orbital"
	orbitalamqp "github.com/openkcm/orbital/client/amqp"
	orbitalcodec "github.com/openkcm/orbital/codec"

	taskv1 "github.com/openkcm/api-sdk/proto/kms/api/cmk/eventprocessor/task/v1"

	eventprocessor "github.com/openkcm/cmk-krypton-bridge/internal/event-processor"
)

// taskCase describes one test scenario.
type taskCase struct {
	name    string
	jobType eventprocessor.JobType
	data    *taskv1.Data
}

func main() {
	amqpURL := flag.String("amqp-url", "amqp://guest:guest@localhost:5672/", "AMQP broker URL")
	amqpTarget := flag.String("amqp-target", "/queues/krypton-bridge.requests", "AMQP target — Bridge listens here")
	amqpSource := flag.String("amqp-source", "/queues/krypton-bridge.replies", "AMQP source — Bridge replies here")
	tenantID := flag.String("tenant-id", "demo-tenant", "Tenant ID for all tasks")
	systemID := flag.String("system-id", "demo-system", "System ID for system-action tasks")
	keyID := flag.String("key-id", "demo-key-id", "Key ID for key-action tasks")
	timeout := flag.Duration("timeout", 60*time.Second, "Total timeout for all tasks")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// ── connect ───────────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Println("│          CMK → Bridge orbital/AMQP demo — all 8 task types          │")
	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Printf("  broker  : %s\n", *amqpURL)
	fmt.Printf("  target  : %s\n", *amqpTarget)
	fmt.Printf("  source  : %s\n", *amqpSource)
	fmt.Printf("  tenant  : %s\n", *tenantID)
	fmt.Printf("  system  : %s\n", *systemID)
	fmt.Printf("  key     : %s\n", *keyID)
	fmt.Println()

	client, err := orbitalamqp.NewClient(ctx, &orbitalcodec.Proto{}, orbitalamqp.ConnectionInfo{
		URL:    *amqpURL,
		Target: *amqpTarget,
		Source: *amqpSource,
	}, orbitalamqp.WithNoAuth())
	if err != nil {
		log.Fatalf("ERROR connect AMQP: %v", err)
	}
	defer client.Close(ctx) //nolint:errcheck

	// ── build test cases ──────────────────────────────────────────────────────
	sysAction := func(keyFrom, keyTo string) *taskv1.Data {
		return &taskv1.Data{
			Data: &taskv1.Data_SystemAction{
				SystemAction: &taskv1.SystemAction{
					SystemId:        *systemID,
					SystemRegion:    "cf-eu10",
					SystemType:      "krypton",
					KeyIdFrom:       keyFrom,
					KeyIdTo:         keyTo,
					KeyProvider:     "gardener",
					TenantId:        *tenantID,
					TenantOwnerId:   "demo-owner",
					TenantOwnerType: "ownerType1",
					CmkRegion:       "cf-eu10",
				},
			},
		}
	}
	keyAction := func() *taskv1.Data {
		return &taskv1.Data{
			Data: &taskv1.Data_KeyAction{
				KeyAction: &taskv1.KeyAction{
					KeyId:     *keyID,
					TenantId:  *tenantID,
					CmkRegion: "cf-eu10",
				},
			},
		}
	}

	cases := []taskCase{
		{
			name:    "SYSTEM_LINK   — link system to key",
			jobType: eventprocessor.JobTypeSystemLink,
			data:    sysAction("", *keyID),
		},
		{
			name:    "SYSTEM_SWITCH — switch to new key",
			jobType: eventprocessor.JobTypeSystemSwitch,
			data:    sysAction(*keyID, *keyID+"-new"),
		},
		{
			name:    "SYSTEM_KEY_ROTATE — rotate key material",
			jobType: eventprocessor.JobTypeSystemKeyRotate,
			data:    sysAction(*keyID, *keyID),
		},
		{
			name:    "SYSTEM_UNLINK — unlink system from key",
			jobType: eventprocessor.JobTypeSystemUnlink,
			data:    sysAction(*keyID, ""),
		},
		{
			name:    "KEY_ENABLE    — enable key",
			jobType: eventprocessor.JobTypeKeyEnable,
			data:    keyAction(),
		},
		{
			name:    "KEY_DISABLE   — disable key",
			jobType: eventprocessor.JobTypeKeyDisable,
			data:    keyAction(),
		},
		{
			name:    "KEY_DETACH    — detach key",
			jobType: eventprocessor.JobTypeKeyDetach,
			data:    keyAction(),
		},
		{
			name:    "KEY_DELETE    — delete key",
			jobType: eventprocessor.JobTypeKeyDelete,
			data:    keyAction(),
		},
	}

	// ── run each task ─────────────────────────────────────────────────────────
	type result struct {
		name   string
		status string
		err    string
		took   time.Duration
	}
	results := make([]result, 0, len(cases))

	for i, tc := range cases {
		fmt.Printf("  [%d/%d] %-44s ", i+1, len(cases), tc.name)

		taskDataBytes, err := proto.Marshal(tc.data)
		if err != nil {
			log.Fatalf("marshal task data: %v", err)
		}

		taskID := uuid.New()
		req := orbital.TaskRequest{
			TaskID:     taskID,
			Type:       tc.jobType.String(),
			ExternalID: fmt.Sprintf("demo-%s", taskID),
			Data:       taskDataBytes,
		}

		start := time.Now()
		if err := client.SendTaskRequest(ctx, req); err != nil {
			log.Fatalf("send task: %v", err)
		}

		resp, err := client.ReceiveTaskResponse(ctx)
		took := time.Since(start)
		if err != nil {
			fmt.Printf("ERROR ✗\n")
			results = append(results, result{tc.name, "ERROR", err.Error(), took})
			continue
		}

		if resp.Status == "DONE" {
			fmt.Printf("DONE  ✓  (%s)\n", took.Round(time.Millisecond))
		} else {
			fmt.Printf("%-6s ✗  (%s)  %s\n", resp.Status, took.Round(time.Millisecond), resp.ErrorMessage)
		}
		results = append(results, result{tc.name, string(resp.Status), resp.ErrorMessage, took})
	}

	// ── summary ───────────────────────────────────────────────────────────────
	passed, failed := 0, 0
	for _, r := range results {
		if r.status == "DONE" {
			passed++
		} else {
			failed++
		}
	}

	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Printf("│  Results: %d/%d passed", passed, len(results))
	padding := 54 - len(fmt.Sprintf("  Results: %d/%d passed", passed, len(results)))
	for range padding {
		fmt.Print(" ")
	}
	fmt.Println("│")
	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")

	if failed > 0 {
		fmt.Println()
		for _, r := range results {
			if r.status != "DONE" {
				fmt.Printf("  FAIL %s: %s\n", r.name, r.err)
			}
		}
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ All task types accepted and completed by KryptonReconciler")
}
