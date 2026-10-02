package eventprocessor

import (
	"context"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"

	"github.com/openkcm/orbital"

	taskv1 "github.com/openkcm/api-sdk/proto/kms/api/cmk/eventprocessor/task/v1"
)

// SystemLinkJobHandler handles SYSTEM_LINK orbital tasks.
// POC: logs and completes — real Krypton integration is a future step.
type SystemLinkJobHandler struct {
	lg *slog.Logger
}

func NewSystemLinkJobHandler(lg *slog.Logger) *SystemLinkJobHandler {
	return &SystemLinkJobHandler{lg: lg}
}

func (h *SystemLinkJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.InfoContext(ctx, "orbital: SYSTEM_LINK (mock)",
		slog.String("taskID", req.TaskID.String()),
		slog.String("tenantID", sa.GetTenantId()),
		slog.String("systemID", sa.GetSystemId()),
		slog.String("systemRegion", sa.GetSystemRegion()),
		slog.String("keyIdTo", sa.GetKeyIdTo()),
		slog.String("keyProvider", sa.GetKeyProvider()))
	resp.Complete()
}

// SystemUnlinkJobHandler handles SYSTEM_UNLINK orbital tasks.
type SystemUnlinkJobHandler struct {
	lg *slog.Logger
}

func NewSystemUnlinkJobHandler(lg *slog.Logger) *SystemUnlinkJobHandler {
	return &SystemUnlinkJobHandler{lg: lg}
}

func (h *SystemUnlinkJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.InfoContext(ctx, "orbital: SYSTEM_UNLINK (mock)",
		slog.String("taskID", req.TaskID.String()),
		slog.String("tenantID", sa.GetTenantId()),
		slog.String("systemID", sa.GetSystemId()),
		slog.String("keyIdFrom", sa.GetKeyIdFrom()))
	resp.Complete()
}

// SystemSwitchJobHandler handles SYSTEM_SWITCH orbital tasks.
type SystemSwitchJobHandler struct {
	lg *slog.Logger
}

func NewSystemSwitchJobHandler(lg *slog.Logger) *SystemSwitchJobHandler {
	return &SystemSwitchJobHandler{lg: lg}
}

func (h *SystemSwitchJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.InfoContext(ctx, "orbital: SYSTEM_SWITCH (mock)",
		slog.String("taskID", req.TaskID.String()),
		slog.String("tenantID", sa.GetTenantId()),
		slog.String("systemID", sa.GetSystemId()),
		slog.String("keyIdFrom", sa.GetKeyIdFrom()),
		slog.String("keyIdTo", sa.GetKeyIdTo()))
	resp.Complete()
}

// SystemKeyRotateJobHandler handles SYSTEM_KEY_ROTATE orbital tasks.
type SystemKeyRotateJobHandler struct {
	lg *slog.Logger
}

func NewSystemKeyRotateJobHandler(lg *slog.Logger) *SystemKeyRotateJobHandler {
	return &SystemKeyRotateJobHandler{lg: lg}
}

func (h *SystemKeyRotateJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.InfoContext(ctx, "orbital: SYSTEM_KEY_ROTATE (mock)",
		slog.String("taskID", req.TaskID.String()),
		slog.String("tenantID", sa.GetTenantId()),
		slog.String("systemID", sa.GetSystemId()),
		slog.String("keyIdTo", sa.GetKeyIdTo()))
	resp.Complete()
}

// ── shared proto unmarshal helper ─────────────────────────────────────────────

func unmarshalSystemActionProto(
	ctx context.Context,
	lg *slog.Logger,
	req orbital.HandlerRequest,
	resp *orbital.HandlerResponse,
) *taskv1.SystemAction {
	var data taskv1.Data
	if err := proto.Unmarshal(req.TaskData, &data); err != nil {
		setErrorStateAndFail(ctx, resp, err, "invalid task data")
		return nil
	}
	sa := data.GetSystemAction()
	if sa == nil {
		setErrorStateAndFail(ctx, resp,
			fmt.Errorf("no SystemAction in task type %s", req.TaskType),
			"missing payload")
		return nil
	}
	return sa
}
