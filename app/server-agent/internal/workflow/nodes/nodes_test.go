package nodes_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
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
// DecideNode is now a passthrough — it always returns success.
// Routing is done by the Runner via the workflow def.

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

// --- ToolCallNode ---

func TestToolCallNode_NoPendingTool_Decide(t *testing.T) {
	n := nodes.NewToolCallNode(nodes.NoopToolRegistry{})
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
	n := nodes.NewToolCallNode(nodes.NoopToolRegistry{})
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
}

// errorToolRegistry always returns an error.
type errorToolRegistry struct{}

func (errorToolRegistry) Invoke(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("tool unavailable")
}

func TestToolCallNode_Error_Resync(t *testing.T) {
	n := nodes.NewToolCallNode(errorToolRegistry{})
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
