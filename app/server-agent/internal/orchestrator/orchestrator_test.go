package orchestrator_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// --- test workflow defs ---

// anyEventTerminalDef routes any event immediately to terminal (success path).
func anyEventTerminalDef(name string) *domain.WorkflowDef {
	return &domain.WorkflowDef{
		Name:  name,
		Entry: "start",
		Steps: map[string]domain.StepDef{
			"start": {
				Trigger:   domain.EventMatch{}, // matches any event
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
}

// noMatchDef's entry step never fires because the trigger kind never appears in tests.
func noMatchDef(name string) *domain.WorkflowDef {
	return &domain.WorkflowDef{
		Name:  name,
		Entry: "wait",
		Steps: map[string]domain.StepDef{
			"wait": {
				Trigger:   domain.EventMatch{Kind: "nonexistent.event"},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
}

// actionDef has an entry step with an action and an expect, so the engine
// dispatches a command and suspends waiting for the confirm event. This lets
// tests use a blocking dispatcher to hold the goroutine in the device lane.
func actionDef(name string) *domain.WorkflowDef {
	return &domain.WorkflowDef{
		Name:  name,
		Entry: "start",
		Steps: map[string]domain.StepDef{
			"start": {
				Trigger: domain.EventMatch{Kind: domain.EventKindAgentOnline},
				Action:  &domain.ActionDef{Kind: domain.ActionKindObserve},
				Expect:  &domain.ExpectDef{Kind: domain.EventKindScreenChanged},
				Timeout: "30s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
}

// --- dispatcher fakes ---

type noopDispatcher struct{}

func (noopDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	ch := make(chan domain.CommandResult, 1)
	ch <- domain.CommandResult{CommandID: cmd.ID, Success: true}
	close(ch)
	return ch, nil
}

func (noopDispatcher) DeliverResponse(domain.CommandResult) {}

// ensure compile-time satisfaction
var _ dispatcher.Dispatcher = noopDispatcher{}

type errorDispatcher struct{ err error }

func (d errorDispatcher) Dispatch(_ context.Context, _ domain.Command) (<-chan domain.CommandResult, error) {
	return nil, d.err
}

func (d errorDispatcher) DeliverResponse(domain.CommandResult) {}

// blockingDispatcher blocks inside Dispatch until release is closed.
// It signals entry via entered so callers can synchronize.
type blockingDispatcher struct {
	entered chan struct{}
	release chan struct{}
}

func (d *blockingDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	select {
	case d.entered <- struct{}{}:
	default:
	}
	<-d.release
	ch := make(chan domain.CommandResult, 1)
	ch <- domain.CommandResult{CommandID: cmd.ID, Success: true}
	close(ch)
	return ch, nil
}

func (d *blockingDispatcher) DeliverResponse(domain.CommandResult) {}

// --- helpers ---

func seededEngine(def *domain.WorkflowDef, disp dispatcher.Dispatcher) *workflow.Engine {
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), def.Name, def)
	return workflow.NewEngine(mem, disp)
}

func newLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newOrch(engine *workflow.Engine, tasks store.TaskStore, states store.WorkflowStateStore) *orchestrator.Orchestrator {
	return orchestrator.New(tasks, states, engine, newLog())
}

func newOrchWithEventStore(
	engine *workflow.Engine,
	tasks store.TaskStore,
	states store.WorkflowStateStore,
	events store.EventPlaneStore,
) *orchestrator.Orchestrator {
	return orchestrator.New(tasks, states, engine, newLog(), events)
}

func runningTask(id, deviceID, workflowName string) *domain.Task {
	return &domain.Task{
		ID:             domain.TaskID(id),
		Status:         domain.TaskStatusRunning,
		AssignedDevice: domain.DeviceID(deviceID),
		WorkflowName:   workflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

// --- tests ---

func TestProcessEvent_BootstrapsWorkflowState(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	def := anyEventTerminalDef("test-default")
	task := runningTask("task-1", "dev-1", def.Name)
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(seededEngine(def, noopDispatcher{}), tasks, states)

	err := orch.ProcessEvent(context.Background(), domain.Event{
		ID: "ev-1", Kind: domain.EventKindAgentOnline,
		DeviceID: "dev-1", SeqNo: 1, OccurredAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	ws, err := states.Get(context.Background(), "task-1", "dev-1")
	if err != nil {
		t.Fatalf("state not found: %v", err)
	}
	if ws.CurrentStep != "terminal" {
		t.Errorf("expected terminal step after anyEventTerminalDef, got %q", ws.CurrentStep)
	}
	if ws.Revision == 0 {
		t.Errorf("expected checkpoint revision to advance, got %d", ws.Revision)
	}
}

func TestProcessEvent_StaleEventDropped(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	def := anyEventTerminalDef("test-default")
	task := runningTask("task-2", "dev-2", def.Name)
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(seededEngine(def, noopDispatcher{}), tasks, states)
	ctx := context.Background()
	base := domain.Event{Kind: domain.EventKindScreenChanged, DeviceID: "dev-2", OccurredAt: time.Now()}

	ev5 := base
	ev5.ID = "ev-5"
	ev5.SeqNo = 5
	_ = orch.ProcessEvent(ctx, ev5)

	ev3 := base
	ev3.ID = "ev-3"
	ev3.SeqNo = 3
	if err := orch.ProcessEvent(ctx, ev3); !errors.Is(err, domain.ErrEventDropped) {
		t.Fatalf("expected dropped error for stale event, got: %v", err)
	}
}

func TestProcessEvent_DuplicateEventDropped(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	def := anyEventTerminalDef("test-default")
	task := runningTask("task-3", "dev-3", def.Name)
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(seededEngine(def, noopDispatcher{}), tasks, states)
	ctx := context.Background()

	ev := domain.Event{ID: "ev-same", Kind: domain.EventKindScreenChanged, DeviceID: "dev-3", SeqNo: 1, OccurredAt: time.Now()}
	_ = orch.ProcessEvent(ctx, ev)

	if err := orch.ProcessEvent(ctx, ev); !errors.Is(err, domain.ErrEventDropped) {
		t.Fatalf("expected dropped error for duplicate event, got: %v", err)
	}
}

func TestProcessAcceptedEvent_RecordsIngestLagMetric(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	metrics := telemetry.NewRegistry()

	def := anyEventTerminalDef("test-default")
	orch := newOrch(seededEngine(def, noopDispatcher{}), tasks, states)
	orch.SetOperationalMetrics(metrics)

	err := orch.ProcessAcceptedEvent(context.Background(), domain.Event{
		ID:         "ev-ingest-lag",
		Kind:       domain.EventKindAccessibilityDisabled,
		DeviceID:   "dev-lag",
		SeqNo:      9,
		OccurredAt: time.Now().Add(-2 * time.Second),
	})
	if err != nil {
		t.Fatalf("ProcessAcceptedEvent: %v", err)
	}

	body := metrics.RenderPrometheus()
	if !strings.Contains(body, `autosdk_server_event_ingest_lag_seconds_count{source="device"} 1`) {
		t.Fatalf("expected ingest lag metric, got:\n%s", body)
	}
}

func TestProcessAcceptedEvent_TracksActiveDeviceLaneGauge(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	metrics := telemetry.NewRegistry()

	def := actionDef("blocking-wf")
	task := runningTask("task-lane-metric", "dev-lane-metric", def.Name)
	_ = tasks.Save(context.Background(), task)

	disp := &blockingDispatcher{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	orch := newOrch(seededEngine(def, disp), tasks, states)
	orch.SetOperationalMetrics(metrics)

	done := make(chan error, 1)
	go func() {
		done <- orch.ProcessAcceptedEvent(context.Background(), domain.Event{
			ID:         "ev-lane-metric",
			Kind:       domain.EventKindAgentOnline,
			DeviceID:   "dev-lane-metric",
			OccurredAt: time.Now(),
		})
	}()

	select {
	case <-disp.entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatcher entry")
	}
	if got := metrics.Snapshot().DeviceLaneActive; got != 1 {
		t.Fatalf("expected active device lane gauge 1, got %d", got)
	}

	close(disp.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ProcessAcceptedEvent: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ProcessAcceptedEvent completion")
	}
	if got := metrics.Snapshot().DeviceLaneActive; got != 0 {
		t.Fatalf("expected active device lane gauge 0 after completion, got %d", got)
	}
}

func TestProcessEvent_TerminalSetsTaskCompleted(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	def := anyEventTerminalDef("test-default")
	task := runningTask("task-4", "dev-4", def.Name)
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(seededEngine(def, noopDispatcher{}), tasks, states)

	ev := domain.Event{ID: "ev-term", Kind: domain.EventKindAgentOnline, DeviceID: "dev-4", SeqNo: 1, OccurredAt: time.Now()}
	if err := orch.ProcessEvent(context.Background(), ev); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	updated, err := tasks.Get(context.Background(), "task-4")
	if err != nil {
		t.Fatalf("get task error: %v", err)
	}
	if updated.Status != domain.TaskStatusCompleted && updated.Status != domain.TaskStatusFailed {
		t.Errorf("expected terminal status, got %s", updated.Status)
	}
}

func TestProcessEvent_EngineError_RecordsDeadLetter(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()

	// Workflow whose action always fails — the dispatcher returns an error,
	// which the engine surfaces as a failure. After MaxRetry exhaustion the
	// engine routes to OnFailure ("terminal"), so no dead-letter from the
	// engine path itself. To force a dead-letter we give the task a workflow
	// name that does not exist in the def store.
	def := anyEventTerminalDef("real-wf")
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), def.Name, def)
	engine := workflow.NewEngine(mem, noopDispatcher{})

	task := runningTask("task-dead-letter", "dev-dead-letter", "missing-workflow")
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(engine, tasks, states, events)

	err := orch.ProcessEvent(context.Background(), domain.Event{
		ID:         "ev-dead-letter",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected process error for missing workflow def")
	}

	deadLetters, err := events.ListDeadLetters(context.Background())
	if err != nil {
		t.Fatalf("ListDeadLetters: %v", err)
	}
	if len(deadLetters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(deadLetters))
	}
	if deadLetters[0].EventID != "ev-dead-letter" {
		t.Fatalf("unexpected dead letter event id: %q", deadLetters[0].EventID)
	}

	// The task must be failed — not left stuck in running.
	updated, err := tasks.Get(context.Background(), "task-dead-letter")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updated.Status != domain.TaskStatusFailed {
		t.Errorf("expected task status failed after missing workflow def, got %s", updated.Status)
	}
}

func TestProcessEvent_MissingWorkflowDef_TaskNotRetriedAfterFail(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	mem := workflow.NewMemoryDefStore()
	engine := workflow.NewEngine(mem, noopDispatcher{})

	task := runningTask("task-no-retry", "dev-no-retry", "missing-workflow")
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(engine, tasks, states)

	ev := func(id string, seq uint64) domain.Event {
		return domain.Event{
			ID: id, Kind: domain.EventKindAgentOnline,
			DeviceID: "dev-no-retry", SeqNo: seq, OccurredAt: time.Now(),
		}
	}

	// First event: workflow def not found → task failed.
	err := orch.ProcessEvent(context.Background(), ev("ev-1", 1))
	if err == nil {
		t.Fatal("expected error for missing workflow def")
	}
	after1, _ := tasks.Get(context.Background(), "task-no-retry")
	if after1.Status != domain.TaskStatusFailed {
		t.Fatalf("expected failed after first event, got %s", after1.Status)
	}

	// Second event: task is now terminal — must be silently skipped (no error, no state).
	if err := orch.ProcessEvent(context.Background(), ev("ev-2", 2)); err != nil {
		t.Fatalf("expected no error on second event for terminal task, got: %v", err)
	}
	after2, _ := tasks.Get(context.Background(), "task-no-retry")
	if after2.Status != domain.TaskStatusFailed {
		t.Errorf("status must remain failed, got %s", after2.Status)
	}
}
