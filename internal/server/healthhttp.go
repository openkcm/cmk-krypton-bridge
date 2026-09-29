package server

import (
	"context"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
)

// HealthHTTPServer provides /healthz and /readyz endpoints for Kubernetes probes.
type HealthHTTPServer struct {
	client kryptonclient.Client
	log    *zap.Logger
}

func NewHealthHTTPServer(client kryptonclient.Client, log *zap.Logger) *HealthHTTPServer {
	return &HealthHTTPServer{client: client, log: log}
}

func (h *HealthHTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", h.liveness)
	mux.HandleFunc("/readyz", h.readiness)
	return mux
}

func (h *HealthHTTPServer) liveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (h *HealthHTTPServer) readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := h.client.Health(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "not ready", "error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

// StartHealthServer starts the HTTP health server and returns when ctx is done.
func StartHealthServer(ctx context.Context, addr string, h http.Handler, log *zap.Logger) {
	srv := &http.Server{Addr: addr, Handler: h}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()
	log.Info("health HTTP server listening", zap.String("address", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("health server error", zap.Error(err))
	}
}
