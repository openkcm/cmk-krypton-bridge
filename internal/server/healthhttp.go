package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/openkcm/cmk-krypton-bridge/internal/kryptonclient"
)

const healthStatusField = "status"

// HealthHTTPServer provides /healthz and /readyz endpoints for Kubernetes probes.
type HealthHTTPServer struct {
	client kryptonclient.Client
	log    *slog.Logger
}

func NewHealthHTTPServer(client kryptonclient.Client, log *slog.Logger) *HealthHTTPServer {
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
	h.writeJSON(w, r, map[string]string{healthStatusField: "ok"})
}

func (h *HealthHTTPServer) readiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := h.client.Health(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		h.writeJSON(w, r, map[string]string{healthStatusField: "not ready", "error": err.Error()})
		return
	}
	h.writeJSON(w, r, map[string]string{healthStatusField: "ready"})
}

func (h *HealthHTTPServer) writeJSON(w http.ResponseWriter, r *http.Request, body map[string]string) {
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.log.ErrorContext(r.Context(), "failed to write health response", slog.String("error", err.Error()))
	}
}

// StartHealthServer starts the HTTP health server and returns when ctx is done.
func StartHealthServer(ctx context.Context, addr string, h http.Handler, log *slog.Logger) {
	srv := &http.Server{Addr: addr, Handler: h}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()
	log.InfoContext(ctx, "health HTTP server listening", slog.String("address", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.ErrorContext(ctx, "health server error", slog.String("error", err.Error()))
	}
}
