package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

type stubNodeHandler struct {
	out NodeOutput
	err error
}

func (h stubNodeHandler) Run(_ context.Context, _ NodeInput) (NodeOutput, error) {
	if h.err != nil {
		return NodeOutput{}, h.err
	}
	return h.out, nil
}

func TestRunnerRun_RecordsWorkflowNodeSuccessMetric(t *testing.T) {
	defs := NewMemoryDefStore()
	if err := defs.Put(context.Background(), DefaultWorkflowDef.Name, DefaultWorkflowDef); err != nil {
		t.Fatalf("put default workflow: %v", err)
	}

	metrics := telemetry.NewRegistry()
	runner := NewRunner(map[domain.NodeKind]NodeHandler{
		domain.NodeKindObserve: stubNodeHandler{out: NodeOutput{Status: NodeStatusSuccess}},
	}, defs, DefaultWorkflowName, metrics)

	state := domain.NewWorkflowState("task-1", "dev-1")
	event := domain.Event{
		ID:         "ev-runner-success",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   "dev-1",
		OccurredAt: time.Now(),
	}
	task := &domain.Task{ID: "task-1", WorkflowName: DefaultWorkflowName}

	if _, _, _, err := runner.Run(context.Background(), NodeInput{Event: event, State: state, Task: task}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	body := metrics.RenderPrometheus()
	if !strings.Contains(body, `autosdk_server_workflow_node_duration_seconds_count{node="observe",outcome="success"} 1`) {
		t.Fatalf("expected success node metric, got:\n%s", body)
	}
}

func TestRunnerRun_RecordsWorkflowNodeErrorMetric(t *testing.T) {
	defs := NewMemoryDefStore()
	if err := defs.Put(context.Background(), DefaultWorkflowDef.Name, DefaultWorkflowDef); err != nil {
		t.Fatalf("put default workflow: %v", err)
	}

	metrics := telemetry.NewRegistry()
	runner := NewRunner(map[domain.NodeKind]NodeHandler{
		domain.NodeKindObserve: stubNodeHandler{err: errors.New("boom")},
	}, defs, DefaultWorkflowName, metrics)

	state := domain.NewWorkflowState("task-2", "dev-2")
	event := domain.Event{
		ID:         "ev-runner-error",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   "dev-2",
		OccurredAt: time.Now(),
	}
	task := &domain.Task{ID: "task-2", WorkflowName: DefaultWorkflowName}

	if _, _, _, err := runner.Run(context.Background(), NodeInput{Event: event, State: state, Task: task}); err == nil {
		t.Fatal("expected runner error")
	}

	body := metrics.RenderPrometheus()
	if !strings.Contains(body, `autosdk_server_workflow_node_duration_seconds_count{node="observe",outcome="error"} 1`) {
		t.Fatalf("expected error node metric, got:\n%s", body)
	}
}
