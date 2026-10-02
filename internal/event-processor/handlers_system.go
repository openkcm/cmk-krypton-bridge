package eventprocessor

import (
	"context"
	"fmt"

	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"

	"github.com/openkcm/orbital"

	taskv1 "github.com/openkcm/api-sdk/proto/kms/api/cmk/eventprocessor/task/v1"
)

// SystemLinkJobHandler handles SYSTEM_LINK orbital tasks.
// POC: logs and completes — real Krypton integration is a future step.
type SystemLinkJobHandler struct {
	lg *zap.Logger
}

func NewSystemLinkJobHandler(lg *zap.Logger) *SystemLinkJobHandler {
	return &SystemLinkJobHandler{lg: lg}
}

func (h *SystemLinkJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.Info("orbital: SYSTEM_LINK (mock)",
		zap.String("taskID", req.TaskID.String()),
		zap.String("tenantID", sa.GetTenantId()),
		zap.String("systemID", sa.GetSystemId()),
		zap.String("systemRegion", sa.GetSystemRegion()),
		zap.String("keyIdTo", sa.GetKeyIdTo()),
		zap.String("keyProvider", sa.GetKeyProvider()))
	resp.Complete()
}

// SystemUnlinkJobHandler handles SYSTEM_UNLINK orbital tasks.
type SystemUnlinkJobHandler struct {
	lg *zap.Logger
}

func NewSystemUnlinkJobHandler(lg *zap.Logger) *SystemUnlinkJobHandler {
	return &SystemUnlinkJobHandler{lg: lg}
}

func (h *SystemUnlinkJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.Info("orbital: SYSTEM_UNLINK (mock)",
		zap.String("taskID", req.TaskID.String()),
		zap.String("tenantID", sa.GetTenantId()),
		zap.String("systemID", sa.GetSystemId()),
		zap.String("keyIdFrom", sa.GetKeyIdFrom()))
	resp.Complete()
}

// SystemSwitchJobHandler handles SYSTEM_SWITCH orbital tasks.
type SystemSwitchJobHandler struct {
	lg *zap.Logger
}

func NewSystemSwitchJobHandler(lg *zap.Logger) *SystemSwitchJobHandler {
	return &SystemSwitchJobHandler{lg: lg}
}

func (h *SystemSwitchJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.Info("orbital: SYSTEM_SWITCH (mock)",
		zap.String("taskID", req.TaskID.String()),
		zap.String("tenantID", sa.GetTenantId()),
		zap.String("systemID", sa.GetSystemId()),
		zap.String("keyIdFrom", sa.GetKeyIdFrom()),
		zap.String("keyIdTo", sa.GetKeyIdTo()))
	resp.Complete()
}

// SystemKeyRotateJobHandler handles SYSTEM_KEY_ROTATE orbital tasks.
type SystemKeyRotateJobHandler struct {
	lg *zap.Logger
}

func NewSystemKeyRotateJobHandler(lg *zap.Logger) *SystemKeyRotateJobHandler {
	return &SystemKeyRotateJobHandler{lg: lg}
}

func (h *SystemKeyRotateJobHandler) Handle(ctx context.Context, req orbital.HandlerRequest, resp *orbital.HandlerResponse) {
	sa := unmarshalSystemActionProto(ctx, h.lg, req, resp)
	if sa == nil {
		return
	}
	h.lg.Info("orbital: SYSTEM_KEY_ROTATE (mock)",
		zap.String("taskID", req.TaskID.String()),
		zap.String("tenantID", sa.GetTenantId()),
		zap.String("systemID", sa.GetSystemId()),
		zap.String("keyIdTo", sa.GetKeyIdTo()))
	resp.Complete()
}

// ── shared proto unmarshal helper ─────────────────────────────────────────────

func unmarshalSystemActionProto(
	ctx context.Context,
	lg *zap.Logger,
	req orbital.HandlerRequest,
	resp *orbital.HandlerResponse,
) *taskv1.SystemAction {
	var data taskv1.Data
	if err := proto.Unmarshal(req.TaskData, &data); err != nil {
		setErrorStateAndFail(ctx, lg, resp, err, "invalid task data")
		return nil
	}
	sa := data.GetSystemAction()
	if sa == nil {
		setErrorStateAndFail(ctx, lg, resp,
			fmt.Errorf("no SystemAction in task type %s", req.TaskType),
			"missing payload")
		return nil
	}
	return sa
}
