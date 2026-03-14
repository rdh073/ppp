package nodes_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

// --- helpers ---

func newState(taskID, deviceID string) *domain.WorkflowState {
	ws := domain.NewWorkflowState(domain.TaskID(taskID), domain.DeviceID(deviceID))
	return ws
}

func newTask(id string) *domain.Task {
	return &domain.Task{
		ID:        domain.TaskID(id),
		Goal:      "test-goal",
		Status:    domain.TaskStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func snapshotRaw(packageName string, targets ...domain.UiTarget) string {
	raw, _ := json.Marshal(domain.UiSnapshot{
		ID:          "snap-test",
		DeviceID:    "dev-test",
		PackageName: packageName,
		CapturedAt:  time.Now(),
		Targets:     targets,
	})
	return string(raw)
}

func executeRaw(snapshotBefore string, snapshotAfter string) string {
	before := json.RawMessage(snapshotBefore)
	after := json.RawMessage(snapshotAfter)
	raw, _ := json.Marshal(map[string]json.RawMessage{
		"snapshotBefore": before,
		"snapshotAfter":  after,
	})
	return string(raw)
}

// fakeDispatcher implements dispatcher.Dispatcher for tests.
type fakeDispatcher struct {
	result domain.CommandResult
}

func (f *fakeDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	ch := make(chan domain.CommandResult, 1)
	res := f.result
	res.CommandID = cmd.ID
	ch <- res
	close(ch)
	return ch, nil
}

func (f *fakeDispatcher) DeliverResponse(_ domain.CommandResult) {}

// errorDispatcher always returns an error from Dispatch.
type errorDispatcher struct{}

func (errorDispatcher) Dispatch(_ context.Context, _ domain.Command) (<-chan domain.CommandResult, error) {
	return nil, errors.New("dispatch failed")
}
func (errorDispatcher) DeliverResponse(_ domain.CommandResult) {}

// --- DecideNode ---
// DecideNode is mostly a passthrough, but built-in workflows may seed
// artifacts such as pending_tool_binding or pending_action.

func TestDecideNode_GoalReached_Terminal(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-1", "dev-1")
	state.Artifacts["goal_reached"] = "true"

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-1")})
	if err != nil {
		t.Fatal(err)
	}
	// DecideNode is a passthrough; routing now lives in the def.
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected NodeStatusSuccess, got %s", out.Status)
	}
}

func TestDecideNode_MaxErrors_Terminal(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-1", "dev-1")
	state.ErrorCount = 5

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-1")})
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected NodeStatusSuccess (passthrough), got %s", out.Status)
	}
}

func TestDecideNode_PendingAction_Act(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-1", "dev-1")
	state.Artifacts["pending_action"] = `{"type":"tap"}`

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-1")})
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected NodeStatusSuccess (passthrough), got %s", out.Status)
	}
}

func TestDecideNode_Default_Observe(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-1", "dev-1")

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-1")})
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected NodeStatusSuccess (passthrough), got %s", out.Status)
	}
}

func TestDecideNode_PrivateDNS_QueuesOpenSettings(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-private-dns-open", "dev-private-dns")
	state.Artifacts["private_dns_hostname"] = "dns.example.com"
	state.Artifacts["last_observe_raw"] = snapshotRaw("com.example.app")
	task := newTask("t-private-dns-open")
	task.WorkflowName = workflow.AndroidSettingsPrivateDNSWorkflowName

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: task})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Fatalf("expected success, got %s", out.Status)
	}
	if out.Artifacts["private_dns_step"] != "open_settings" {
		t.Fatalf("expected open_settings step, got %q", out.Artifacts["private_dns_step"])
	}
	if !strings.Contains(out.Artifacts["pending_action"], `"kind":"open_app"`) {
		t.Fatalf("expected open_app action, got %s", out.Artifacts["pending_action"])
	}
	if !strings.Contains(out.Artifacts["pending_action"], `"value":"com.android.settings"`) {
		t.Fatalf("expected settings package target, got %s", out.Artifacts["pending_action"])
	}
}

func TestDecideNode_PrivateDNS_QueuesHostnameInput(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-private-dns-input", "dev-private-dns")
	state.Artifacts["private_dns_hostname"] = "dns.example.com"
	state.Artifacts["last_observe_raw"] = snapshotRaw("com.android.settings",
		domain.UiTarget{TargetID: "edit-1", ResourceID: "android:id/edit", Enabled: true},
	)
	task := newTask("t-private-dns-input")
	task.WorkflowName = workflow.AndroidSettingsPrivateDNSWorkflowName

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: task})
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifacts["private_dns_step"] != "input_hostname" {
		t.Fatalf("expected input_hostname step, got %q", out.Artifacts["private_dns_step"])
	}
	if !strings.Contains(out.Artifacts["pending_action"], `"kind":"input_text"`) {
		t.Fatalf("expected input_text action, got %s", out.Artifacts["pending_action"])
	}
	if !strings.Contains(out.Artifacts["pending_action"], `"value":"android:id/edit"`) {
		t.Fatalf("expected edit field selector, got %s", out.Artifacts["pending_action"])
	}
	if !strings.Contains(out.Artifacts["pending_action"], `"inputText":"dns.example.com"`) {
		t.Fatalf("expected hostname input payload, got %s", out.Artifacts["pending_action"])
	}
}

func TestDecideNode_PrivateDNS_QueuesSaveWhenHostnamePresent(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-private-dns-save", "dev-private-dns")
	state.Artifacts["private_dns_hostname"] = "dns.example.com"
	state.Artifacts["last_observe_raw"] = snapshotRaw("com.android.settings",
		domain.UiTarget{TargetID: "edit-1", ResourceID: "android:id/edit", Enabled: true},
		domain.UiTarget{TargetID: "host-text", Text: "dns.example.com", Enabled: true},
		domain.UiTarget{TargetID: "save-button", Text: "Save", Actionable: true, Enabled: true},
	)
	task := newTask("t-private-dns-save")
	task.WorkflowName = workflow.AndroidSettingsPrivateDNSWorkflowName

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: task})
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifacts["private_dns_step"] != "save_hostname" {
		t.Fatalf("expected save_hostname step, got %q", out.Artifacts["private_dns_step"])
	}
	if !strings.Contains(out.Artifacts["pending_action"], `"kind":"click"`) {
		t.Fatalf("expected click action, got %s", out.Artifacts["pending_action"])
	}
	if !strings.Contains(out.Artifacts["pending_action"], `"value":"Save"`) {
		t.Fatalf("expected Save selector, got %s", out.Artifacts["pending_action"])
	}
}

func TestDecideNode_PrivateDNS_MarksGoalReachedWhenHostnameVisible(t *testing.T) {
	n := nodes.NewDecideNode()
	state := newState("t-private-dns-done", "dev-private-dns")
	state.Artifacts["private_dns_hostname"] = "dns.example.com"
	state.Artifacts["last_observe_raw"] = snapshotRaw("com.android.settings",
		domain.UiTarget{TargetID: "row-1", Text: "Private DNS", Enabled: true},
		domain.UiTarget{TargetID: "summary-1", Text: "dns.example.com", Enabled: true},
	)
	task := newTask("t-private-dns-done")
	task.WorkflowName = workflow.AndroidSettingsPrivateDNSWorkflowName

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: task})
	if err != nil {
		t.Fatal(err)
	}
	if out.Artifacts["goal_reached"] != "true" {
		t.Fatalf("expected goal_reached=true, got %q", out.Artifacts["goal_reached"])
	}
	if out.Artifacts["terminal_reason"] != "private_dns_hostname_applied" {
		t.Fatalf("expected terminal reason, got %q", out.Artifacts["terminal_reason"])
	}
}

// --- ObserveNode ---

func TestObserveNode_Success_Decide(t *testing.T) {
	disp := &fakeDispatcher{result: domain.CommandResult{
		Success: true,
		Raw:     json.RawMessage(`{"snapshot":"s1"}`),
	}}
	n := nodes.NewObserveNode(disp)
	state := newState("t-2", "dev-2")

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-2")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected NodeStatusSuccess, got %s", out.Status)
	}
	if out.Artifacts["last_observe_raw"] == "" {
		t.Error("last_observe_raw artifact not set")
	}
}

func TestObserveNode_DispatchError_Resync(t *testing.T) {
	n := nodes.NewObserveNode(errorDispatcher{})
	state := newState("t-2", "dev-2")

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-2")})
	if out.Status != workflow.NodeStatusFailure {
		t.Errorf("expected NodeStatusFailure on dispatch error, got %s", out.Status)
	}
}

func TestObserveNode_Failure_Resync(t *testing.T) {
	disp := &fakeDispatcher{result: domain.CommandResult{Success: false}}
	n := nodes.NewObserveNode(disp)
	state := newState("t-2", "dev-2")

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-2")})
	if out.Status != workflow.NodeStatusFailure {
		t.Errorf("expected NodeStatusFailure on failure, got %s", out.Status)
	}
}

func TestVerifyNode_Success_PromotesSnapshotAfterToLastObserveRaw(t *testing.T) {
	n := nodes.NewVerifyNode()
	state := newState("t-verify", "dev-verify")
	before := snapshotRaw("com.android.settings", domain.UiTarget{TargetID: "before", Text: "Private DNS"})
	after := snapshotRaw("com.android.settings", domain.UiTarget{TargetID: "after", Text: "dns.example.com"})
	state.Artifacts["pre_action_snapshot"] = before
	state.Artifacts["last_execute_raw"] = executeRaw(before, after)

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-verify")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Fatalf("expected success, got %s", out.Status)
	}
	if out.Artifacts["last_observe_raw"] != after {
		t.Fatalf("expected last_observe_raw to be snapshotAfter, got %s", out.Artifacts["last_observe_raw"])
	}
}

// --- ToolCallNode ---

func TestToolCallNode_NoPendingTool_Decide(t *testing.T) {
	n := nodes.NewToolCallNode(nodes.DisabledToolRegistry{})
	state := newState("t-3", "dev-3")

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-3")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected NodeStatusSuccess when no tool queued, got %s", out.Status)
	}
}

func TestToolCallNode_Success_Decide(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:          "some_tool",
			Description:   "test tool",
			Deterministic: true,
			Timeout:       time.Second,
			InputSchema:   json.RawMessage(`{"type":"object"}`),
			OutputSchema:  json.RawMessage(`{"type":"object"}`),
		},
		Handler: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			if string(params) != `{"key":"val"}` {
				return nil, errors.New("unexpected params")
			}
			return json.RawMessage(`{"status":"ok"}`), nil
		},
	})
	n := nodes.NewToolCallNode(registry)
	state := newState("t-3", "dev-3")
	state.Artifacts["pending_tool"] = "some_tool"
	state.Artifacts["pending_tool_params"] = `{"key":"val"}`

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-3")})
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected NodeStatusSuccess after tool success, got %s", out.Status)
	}
	// pending_tool should be in DeleteArtifacts
	found := false
	for _, k := range out.DeleteArtifacts {
		if k == "pending_tool" {
			found = true
		}
	}
	if !found {
		t.Error("pending_tool should be in DeleteArtifacts after invocation")
	}
	if out.Artifacts["tool_result"] == "" {
		t.Error("tool_result should be set")
	}
	if len(out.EmittedEvents) != 1 || out.EmittedEvents[0].Kind != domain.EventKindToolResult {
		t.Fatalf("expected one tool.result event, got %+v", out.EmittedEvents)
	}
}

func TestToolCallNode_BindingSuccess_MapsArtifacts(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "bound_tool",
			Timeout: time.Second,
		},
		Handler: func(_ context.Context, params json.RawMessage) (json.RawMessage, error) {
			if string(params) != `{"fullName":"Ayu Lestari"}` {
				return nil, errors.New("unexpected params")
			}
			return json.RawMessage(`{"email":"ayu@example.id","mode":"llm"}`), nil
		},
	})
	bindings := nodes.NewStaticToolBindingResolver(nodes.ToolBinding{
		ID:             "binding.generate_email",
		ToolName:       "bound_tool",
		ParamsTemplate: `{"fullName": {{ json (artifact "profile_full_name") }}}`,
		SuccessArtifacts: []nodes.ToolArtifactBinding{
			{Artifact: "profile_email", FromJSONPointer: "/email"},
			{Artifact: "generation_mode", FromJSONPointer: "/mode"},
		},
	})
	n := nodes.NewToolCallNodeWithBindings(registry, bindings)
	state := newState("t-bind", "dev-bind")
	state.Artifacts["profile_full_name"] = "Ayu Lestari"
	state.Artifacts["pending_tool_binding"] = "binding.generate_email"

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-bind")})
	if out.Status != workflow.NodeStatusSuccess {
		t.Fatalf("expected success, got %s", out.Status)
	}
	if out.Artifacts["profile_email"] != "ayu@example.id" {
		t.Fatalf("expected mapped email, got %q", out.Artifacts["profile_email"])
	}
	if out.Artifacts["last_tool_binding"] != "binding.generate_email" {
		t.Fatalf("expected last_tool_binding to be set, got %q", out.Artifacts["last_tool_binding"])
	}
}

func TestToolCallNode_Success_RecordsToolMetric(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "metric_tool",
			Timeout: time.Second,
		},
		Handler: func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"status":"ok"}`), nil
		},
	})
	metrics := telemetry.NewRegistry()
	n := nodes.NewToolCallNode(registry, metrics)
	state := newState("t-metric", "dev-metric")
	state.Artifacts["pending_tool"] = "metric_tool"

	if _, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-metric")}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	body := metrics.RenderPrometheus()
	if !strings.Contains(body, `autosdk_server_tool_call_duration_seconds_count{tool="metric_tool",outcome="success"} 1`) {
		t.Fatalf("expected tool success metric, got:\n%s", body)
	}
}

func TestToolCallNode_UnsupportedTool_Resync(t *testing.T) {
	n := nodes.NewToolCallNode(nodes.DisabledToolRegistry{})
	state := newState("t-3", "dev-3")
	state.Artifacts["pending_tool"] = "missing_tool"

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-3")})
	if out.Status != workflow.NodeStatusFailure {
		t.Errorf("expected NodeStatusFailure on unsupported tool, got %s", out.Status)
	}
	if out.Artifacts["resync_reason"] == "" {
		t.Error("expected resync_reason to be set on unsupported tool")
	}
}

func TestToolCallNode_Error_Resync(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "broken_tool",
			Timeout: time.Second,
		},
		Handler: func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("tool unavailable")
		},
	})
	n := nodes.NewToolCallNode(registry)
	state := newState("t-3", "dev-3")
	state.Artifacts["pending_tool"] = "broken_tool"

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-3")})
	if out.Status != workflow.NodeStatusFailure {
		t.Errorf("expected NodeStatusFailure on tool error, got %s", out.Status)
	}
	// resync_reason should be set in Artifacts
	if out.Artifacts["resync_reason"] == "" {
		t.Error("expected resync_reason to be set on tool error")
	}
}

func TestToolCallNode_OptionalToolError_ReturnsToolErrorForFallback(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "optional_tool",
			Timeout: time.Second,
		},
		Handler: func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("provider unavailable")
		},
	})
	n := nodes.NewToolCallNode(registry)
	state := newState("t-optional", "dev-optional")
	state.Artifacts["pending_tool"] = "optional_tool"
	state.Artifacts["pending_tool_optional"] = "true"

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-optional")})
	if out.Status != workflow.NodeStatusSuccess {
		t.Fatalf("expected NodeStatusSuccess for optional tool failure, got %s", out.Status)
	}
	if out.Artifacts["tool_error"] == "" {
		t.Fatal("expected tool_error artifact for optional failure")
	}
	if out.Artifacts["last_tool_name"] != "optional_tool" {
		t.Fatalf("expected last_tool_name optional_tool, got %q", out.Artifacts["last_tool_name"])
	}
	if len(out.EmittedEvents) != 1 || out.EmittedEvents[0].Kind != domain.EventKindToolResult {
		t.Fatalf("expected one tool.result event for optional fallback, got %+v", out.EmittedEvents)
	}
}

func TestToolCallNode_BindingOptionalFailure_MapsFallbackArtifacts(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "optional_bound_tool",
			Timeout: time.Second,
		},
		Handler: func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
			return nil, errors.New("provider unavailable")
		},
	})
	bindings := nodes.NewStaticToolBindingResolver(nodes.ToolBinding{
		ID:             "binding.optional",
		ToolName:       "optional_bound_tool",
		Optional:       true,
		ParamsTemplate: `{}`,
		FailureArtifacts: []nodes.ToolArtifactBinding{
			{Artifact: "welcome_email_generation_mode", Value: strPtr("fallback_template")},
			{Artifact: "welcome_email_error", Template: "{{ .ToolError }}"},
		},
	})
	n := nodes.NewToolCallNodeWithBindings(registry, bindings)
	state := newState("t-optional-binding", "dev-optional-binding")
	state.Artifacts["pending_tool_binding"] = "binding.optional"

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-optional-binding")})
	if out.Status != workflow.NodeStatusSuccess {
		t.Fatalf("expected success for optional binding failure, got %s", out.Status)
	}
	if out.Artifacts["welcome_email_generation_mode"] != "fallback_template" {
		t.Fatalf("expected fallback mapping, got %q", out.Artifacts["welcome_email_generation_mode"])
	}
	if out.Artifacts["last_tool_binding"] != "binding.optional" {
		t.Fatalf("expected last_tool_binding, got %q", out.Artifacts["last_tool_binding"])
	}
}

func TestToolCallNode_InvalidResult_Resync(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "invalid_result_tool",
			Timeout: time.Second,
		},
		Handler: func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"status":"ok"}`), nil
		},
		ValidateResult: func(_ json.RawMessage) error {
			return errors.New("missing required field")
		},
	})
	n := nodes.NewToolCallNode(registry)
	state := newState("t-3", "dev-3")
	state.Artifacts["pending_tool"] = "invalid_result_tool"

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-3")})
	if out.Status != workflow.NodeStatusFailure {
		t.Errorf("expected NodeStatusFailure on invalid tool result, got %s", out.Status)
	}
	if out.Artifacts["resync_reason"] == "" {
		t.Error("expected resync_reason to be set on invalid tool result")
	}
}

func strPtr(value string) *string { return &value }

func TestToolCallNode_Timeout_Resync(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "slow_tool",
			Timeout: 10 * time.Millisecond,
		},
		Handler: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	n := nodes.NewToolCallNode(registry)
	state := newState("t-3", "dev-3")
	state.Artifacts["pending_tool"] = "slow_tool"

	out, _ := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-3")})
	if out.Status != workflow.NodeStatusFailure {
		t.Errorf("expected NodeStatusFailure on timeout, got %s", out.Status)
	}
	if out.Artifacts["resync_reason"] == "" {
		t.Error("expected resync_reason to be set on timeout")
	}
}

func TestToolCallNode_Timeout_RecordsTimeoutMetric(t *testing.T) {
	registry := nodes.NewStaticToolRegistry(nodes.ToolDefinition{
		Manifest: nodes.ToolManifest{
			Name:    "metric_timeout_tool",
			Timeout: 10 * time.Millisecond,
		},
		Handler: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	metrics := telemetry.NewRegistry()
	n := nodes.NewToolCallNode(registry, metrics)
	state := newState("t-timeout-metric", "dev-timeout-metric")
	state.Artifacts["pending_tool"] = "metric_timeout_tool"

	if _, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-timeout-metric")}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	body := metrics.RenderPrometheus()
	if !strings.Contains(body, `autosdk_server_tool_call_duration_seconds_count{tool="metric_timeout_tool",outcome="timeout"} 1`) {
		t.Fatalf("expected tool timeout metric, got:\n%s", body)
	}
}

// --- TerminalNode ---

func TestTerminalNode_Done(t *testing.T) {
	n := nodes.NewTerminalNode()
	state := newState("t-4", "dev-4")
	state.Artifacts["goal_reached"] = "true"

	out, err := n.Run(context.Background(), workflow.NodeInput{State: state, Task: newTask("t-4")})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Done {
		t.Error("TerminalNode must set Done=true")
	}
}
