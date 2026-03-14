package telemetry_test

import (
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

func TestRegistrySnapshotAndRenderPrometheus(t *testing.T) {
	reg := telemetry.NewRegistry()
	reg.RecordWakeupFallback(telemetry.WakeupFallbackIngress)
	reg.RecordWakeupFallback(telemetry.WakeupFallbackEmittedBatch)
	reg.RecordLeaseLost()
	reg.RecordReplay(telemetry.ReplayPathAccepted, telemetry.ReplayOutcomeAttempted)
	reg.RecordReplay(telemetry.ReplayPathAccepted, telemetry.ReplayOutcomeSucceeded)
	reg.RecordReplay(telemetry.ReplayPathDeadLetterIngestion, telemetry.ReplayOutcomeAttempted)
	reg.RecordReplay(telemetry.ReplayPathDeadLetterIngestion, telemetry.ReplayOutcomeFailed)
	reg.SetWakeupQueueDepth(7)
	reg.SetWakeupPartitionDepth("p00", 5)
	reg.SetWakeupPartitionDepth("p01", 2)
	reg.EnterDeviceLane()
	reg.IncCommandInflight()
	reg.RecordCommandTimeout("device.observe")
	reg.ObserveIngestLag(telemetry.IngestSourceDevice, 150*time.Millisecond)
	reg.ObserveWorkflowNode("observe", telemetry.WorkflowNodeOutcomeSuccess, 40*time.Millisecond)
	reg.ObserveToolCall("identity.generate_email", telemetry.ToolCallOutcomeSuccess, 20*time.Millisecond)
	reg.ObserveCommand("device.observe", telemetry.CommandOutcomeRespondedSuccess, 90*time.Millisecond)

	snap := reg.Snapshot()
	if snap.WakeupFallback[telemetry.WakeupFallbackIngress] != 1 {
		t.Fatalf("expected ingress fallback count 1, got %d", snap.WakeupFallback[telemetry.WakeupFallbackIngress])
	}
	if snap.LeaseLost != 1 {
		t.Fatalf("expected lease lost count 1, got %d", snap.LeaseLost)
	}
	if snap.WorkflowWakeupQueueDepth != 7 {
		t.Fatalf("expected queue depth 7, got %d", snap.WorkflowWakeupQueueDepth)
	}
	if snap.DeviceLaneActive != 1 {
		t.Fatalf("expected device lane active 1, got %d", snap.DeviceLaneActive)
	}
	if snap.CommandInflight != 1 {
		t.Fatalf("expected command inflight 1, got %d", snap.CommandInflight)
	}
	if snap.WakeupPartitionDepth["p00"] != 5 || snap.WakeupPartitionDepth["p01"] != 2 {
		t.Fatalf("unexpected per-partition depth snapshot: %#v", snap.WakeupPartitionDepth)
	}
	if snap.CommandTimeout["device.observe"] != 1 {
		t.Fatalf("expected command timeout count 1, got %#v", snap.CommandTimeout)
	}
	if snap.Replay[telemetry.ReplayPathAccepted][telemetry.ReplayOutcomeSucceeded] != 1 {
		t.Fatalf("expected accepted replay success 1, got %d", snap.Replay[telemetry.ReplayPathAccepted][telemetry.ReplayOutcomeSucceeded])
	}

	body := reg.RenderPrometheus()
	for _, needle := range []string{
		`autosdk_server_wakeup_publish_fallback_total{path="ingress"} 1`,
		`autosdk_server_redis_partition_lease_lost_total 1`,
		`autosdk_server_workflow_wakeup_queue_depth 7`,
		`autosdk_server_workflow_wakeup_partition_depth{partition="p00"} 5`,
		`autosdk_server_workflow_wakeup_partition_depth{partition="p01"} 2`,
		`autosdk_server_device_lane_active 1`,
		`autosdk_server_command_inflight 1`,
		`autosdk_server_command_timeout_total{kind="device.observe"} 1`,
		`autosdk_server_event_replay_total{path="accepted",outcome="succeeded"} 1`,
		`autosdk_server_event_replay_total{path="dead_letter_ingestion",outcome="failed"} 1`,
		`autosdk_server_event_ingest_lag_seconds_count{source="device"} 1`,
		`autosdk_server_workflow_node_duration_seconds_count{node="observe",outcome="success"} 1`,
		`autosdk_server_tool_call_duration_seconds_count{tool="identity.generate_email",outcome="success"} 1`,
		`autosdk_server_command_duration_seconds_count{kind="device.observe",outcome="responded_success"} 1`,
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected metrics output to contain %q, got:\n%s", needle, body)
		}
	}
}
