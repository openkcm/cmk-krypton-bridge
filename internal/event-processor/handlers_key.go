package eventprocessor

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"

	"github.com/openkcm/orbital"

	taskv1 "github.com/openkcm/api-sdk/proto/kms/api/cmk/eventprocessor/task/v1"
)

// KeyJobHandler handles KEY_ENABLE, KEY_DISABLE, KEY_DELETE orbital tasks.
// Mirrors CMK's KeyJobHandler — POC: no-op, completes immediately.
type KeyJobHandler struct {
	lg *slog.Logger
}

func NewKeyJobHandler(lg *slog.Logger) *KeyJobHandler {
	return &KeyJobHandler{lg: lg}
}

func (h *KeyJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	ka := unmarshalKeyActionProto(ctx, h.lg, req, resp)
	if ka == nil {
		return
	}
	h.lg.InfoContext(ctx, "orbital: key action (mock)",
		slog.String("taskID", req.TaskID.String()),
		slog.String("taskType", req.TaskType),
		slog.String("keyID", ka.GetKeyId()),
		slog.String("tenantID", ka.GetTenantId()),
		slog.String("cmkRegion", ka.GetCmkRegion()))
	resp.Complete()
}

// KeyDetachJobHandler handles KEY_DETACH orbital tasks.
// Mirrors CMK's KeyDetachJobHandler — POC: logs and completes immediately.
type KeyDetachJobHandler struct {
	lg *slog.Logger
}

func NewKeyDetachJobHandler(lg *slog.Logger) *KeyDetachJobHandler {
	return &KeyDetachJobHandler{lg: lg}
}

func (h *KeyDetachJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	ka := unmarshalKeyActionProto(ctx, h.lg, req, resp)
	if ka == nil {
		return
	}
	h.lg.InfoContext(ctx, "orbital: KEY_DETACH (mock)",
		slog.String("taskID", req.TaskID.String()),
		slog.String("keyID", ka.GetKeyId()),
		slog.String("tenantID", ka.GetTenantId()))
	resp.Complete()
}

// ── shared proto unmarshal helper ─────────────────────────────────────────────

func unmarshalKeyActionProto(
	ctx context.Context,
	lg *slog.Logger,
	req orbital.HandlerRequest,
	resp *orbital.HandlerResponse,
) *taskv1.KeyAction {
	var data taskv1.Data
	if err := proto.Unmarshal(req.TaskData, &data); err != nil {
		setErrorStateAndFail(ctx, resp, err, "invalid task data")
		return nil
	}
	ka := data.GetKeyAction()
	if ka == nil {
		setErrorStateAndFail(ctx, resp,
			fmt.Errorf("no KeyAction in task type %s", req.TaskType),
			"missing payload")
		return nil
	}
	return ka
}
