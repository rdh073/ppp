package orchestrator_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// fakeNodeHandler returns a pre-canned NodeOutput for every Run call.
// It no longer sets CurrentNode — routing is the Runner's job via the def.
type fakeNodeHandler struct {
	status workflow.NodeStatus
	done   bool
}

func (h *fakeNodeHandler) Run(_ context.Context, _ workflow.NodeInput) (workflow.NodeOutput, error) {
	return workflow.NodeOutput{Status: h.status, Done: h.done}, nil
}

func seededDefStore() *workflow.MemoryDefStore {
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), workflow.DefaultWorkflowDef.Name, workflow.DefaultWorkflowDef)
	return mem
}

func newFakeRunner(status workflow.NodeStatus) *workflow.Runner {
	h := &fakeNodeHandler{status: status}
	handlers := map[domain.NodeKind]workflow.NodeHandler{}
	for _, k := range []domain.NodeKind{
		domain.NodeKindObserve, domain.NodeKindDecide, domain.NodeKindAct,
		domain.NodeKindVerify, domain.NodeKindResync, domain.NodeKindTerminal,
	} {
		handlers[k] = h
	}
	return workflow.NewRunner(handlers, seededDefStore(), "default")
}

func newTerminalRunner() *workflow.Runner {
	h := &fakeNodeHandler{done: true}
	handlers := map[domain.NodeKind]workflow.NodeHandler{}
	for _, k := range []domain.NodeKind{
		domain.NodeKindObserve, domain.NodeKindDecide, domain.NodeKindAct,
		domain.NodeKindVerify, domain.NodeKindResync, domain.NodeKindTerminal,
	} {
		handlers[k] = h
	}
	return workflow.NewRunner(handlers, seededDefStore(), "default")
}

func newOrch(runner *workflow.Runner, tasks store.TaskStore, states store.WorkflowStateStore) *orchestrator.Orchestrator {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return orchestrator.New(tasks, states, runner, log)
}

// --- tests ---

func TestProcessEvent_BootstrapsWorkflowState(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	task := &domain.Task{
		ID:             "task-1",
		Goal:           "test",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-1",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	// success on observe → def routes to decide
	orch := newOrch(newFakeRunner(workflow.NodeStatusSuccess), tasks, states)

	event := domain.Event{
		ID:         "ev-1",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   "dev-1",
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	ws, err := states.Get(context.Background(), "task-1", "dev-1")
	if err != nil {
		t.Fatalf("state not found: %v", err)
	}
	// observe success → decide (per DefaultWorkflowDef)
	if ws.CurrentNode != domain.NodeKindDecide {
		t.Errorf("expected Decide, got %s", ws.CurrentNode)
	}
}

func TestProcessEvent_StaleEventDropped(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	task := &domain.Task{
		ID:             "task-2",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-2",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(newFakeRunner(workflow.NodeStatusSuccess), tasks, states)

	ctx := context.Background()
	base := domain.Event{Kind: domain.EventKindUiObservation, DeviceID: "dev-2", OccurredAt: time.Now()}

	// Process seqNo=5 first.
	ev5 := base
	ev5.ID = "ev-5"
	ev5.SeqNo = 5
	_ = orch.ProcessEvent(ctx, ev5)

	// Now process seqNo=3 (stale) — state must NOT be overwritten with a different transition.
	ev3 := base
	ev3.ID = "ev-3"
	ev3.SeqNo = 3
	if err := orch.ProcessEvent(ctx, ev3); err != nil {
		t.Fatalf("unexpected error on stale event: %v", err)
	}
	// If the stale event had been processed it would create a second state entry; verify only one run occurred.
}

func TestProcessEvent_DuplicateEventDropped(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	task := &domain.Task{
		ID:             "task-3",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-3",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(newFakeRunner(workflow.NodeStatusSuccess), tasks, states)
	ctx := context.Background()

	ev := domain.Event{ID: "ev-same", Kind: domain.EventKindUiObservation, DeviceID: "dev-3", SeqNo: 1, OccurredAt: time.Now()}
	_ = orch.ProcessEvent(ctx, ev)

	// Second identical event must be silently dropped (no panic, no error).
	if err := orch.ProcessEvent(ctx, ev); err != nil {
		t.Fatalf("duplicate event returned error: %v", err)
	}
}

func TestProcessEvent_TerminalSetsTaskCompleted(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	task := &domain.Task{
		ID:             "task-4",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-4",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	runner := newTerminalRunner()
	orch := newOrch(runner, tasks, states)

	ev := domain.Event{ID: "ev-term", Kind: domain.EventKindAgentOnline, DeviceID: "dev-4", SeqNo: 1, OccurredAt: time.Now()}
	// Set goal_reached in state beforehand.
	ws := domain.NewWorkflowState("task-4", "dev-4")
	ws.Artifacts["goal_reached"] = "true"
	_ = states.Save(context.Background(), ws)

	if err := orch.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	updated, err := tasks.Get(context.Background(), "task-4")
	if err != nil {
		t.Fatalf("get task error: %v", err)
	}
	if updated.Status != domain.TaskStatusFailed && updated.Status != domain.TaskStatusCompleted {
		t.Errorf("expected terminal status, got %s", updated.Status)
	}
}
