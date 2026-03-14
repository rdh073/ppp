package handler

import (
	"net/http"

	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

type MetricsHandler struct {
	registry *telemetry.Registry
}

func NewMetricsHandler(registry *telemetry.Registry) *MetricsHandler {
	return &MetricsHandler{registry: registry}
}

func (h *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.registry.RenderPrometheus()))
}
