// Package server contains the orbital handler scaffold.
// The exact HandlerRequest / HandlerResponse types must be replaced with the
// real types once github.com/openkcm/orbital is available.
package server

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
	"github.com/openkcm/cmk-krypton-bridge/internal/orchestrator"
)

// OrbitalTaskType is the task type sent by the CMK backend over orbital.
const OrbitalTaskType = "cmk-krypton-bridge.ensure-key-hierarchy"

// OrbitalPayload is the payload structure sent by the CMK backend.
type OrbitalPayload struct {
	TenantID  string            `json:"tenantId"`
	ServiceID string            `json:"serviceId"`
	Level     int               `json:"level"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// OrbitalHandlerFunc is a placeholder for the real orbital.HandlerFunc type.
// Replace with: orbital.HandlerFunc once the orbital package is imported.
type OrbitalHandlerFunc func(ctx context.Context, rawPayload []byte) error

// NewOrbitalHandlerFunc creates the handler function for the orbital operator.
func NewOrbitalHandlerFunc(
	orch *orchestrator.Orchestrator,
	log *zap.Logger,
) OrbitalHandlerFunc {
	return func(ctx context.Context, rawPayload []byte) error {
		var payload OrbitalPayload
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return fmt.Errorf("unmarshal orbital payload: %w", err)
		}

		result, err := orch.EnsureHierarchy(ctx, orchestrator.EnsureHierarchyRequest{
			TenantID:  payload.TenantID,
			ServiceID: payload.ServiceID,
			Level:     kryptonclient.Level(payload.Level),
			Metadata:  payload.Metadata,
		})
		if err != nil {
			log.Error("orbital: EnsureHierarchy failed",
				zap.Error(err),
				zap.String("tenantID", payload.TenantID))
			return fmt.Errorf("ensure hierarchy: %w", err)
		}

		log.Info("orbital: EnsureHierarchy succeeded",
			zap.String("keyID", result.RequestedKey.ID),
			zap.Int("createdCount", len(result.CreatedKeys)))
		return nil
	}
}
