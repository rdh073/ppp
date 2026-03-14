package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

func TestMetricsHandler_RendersPrometheusText(t *testing.T) {
	reg := telemetry.NewRegistry()
	reg.RecordWakeupFallback(telemetry.WakeupFallbackIngress)
	reg.SetWakeupQueueDepth(3)
	reg.SetWakeupPartitionDepth("p00", 3)
	reg.EnterDeviceLane()
	reg.IncCommandInflight()
	reg.RecordCommandTimeout("device.observe")
	reg.ObserveIngestLag(telemetry.IngestSourceDevice, 125*time.Millisecond)
	reg.ObserveCommand("device.observe", telemetry.CommandOutcomeRespondedSuccess, 80*time.Millisecond)

	h := handler.NewMetricsHandler(reg)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	body := rec.Body.String()
	for _, needle := range []string{
		`autosdk_server_wakeup_publish_fallback_total{path="ingress"} 1`,
		`autosdk_server_workflow_wakeup_queue_depth 3`,
		`autosdk_server_workflow_wakeup_partition_depth{partition="p00"} 3`,
		`autosdk_server_device_lane_active 1`,
		`autosdk_server_command_inflight 1`,
		`autosdk_server_command_timeout_total{kind="device.observe"} 1`,
		`autosdk_server_event_ingest_lag_seconds_count{source="device"} 1`,
		`autosdk_server_command_duration_seconds_count{kind="device.observe",outcome="responded_success"} 1`,
	} {
		if strings.Contains(body, needle) {
			continue
		}
		t.Fatalf("unexpected metrics body:\n%s", rec.Body.String())
	}
}
