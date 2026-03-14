package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/store"
	toolcatalog "github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
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

type errorNodeHandler struct {
	err error
}

func (h *errorNodeHandler) Run(_ context.Context, _ workflow.NodeInput) (workflow.NodeOutput, error) {
	return workflow.NodeOutput{}, h.err
}

func seededDefStore() *workflow.MemoryDefStore {
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), workflow.DefaultWorkflowDef.Name, workflow.DefaultWorkflowDef)
	_ = mem.Put(context.Background(), workflow.LocalIdentityProfileWorkflowDef.Name, workflow.LocalIdentityProfileWorkflowDef)
	_ = mem.Put(context.Background(), workflow.LocalIdentityWelcomeEmailWorkflowDef.Name, workflow.LocalIdentityWelcomeEmailWorkflowDef)
	return mem
}

func newFakeRunner(status workflow.NodeStatus) *workflow.Runner {
	h := &fakeNodeHandler{status: status}
	handlers := map[domain.NodeKind]workflow.NodeHandler{}
	for _, k := range []domain.NodeKind{
		domain.NodeKindObserve, domain.NodeKindDecide, domain.NodeKindAct,
		domain.NodeKindVerify, domain.NodeKindResync, domain.NodeKindToolCall,
		domain.NodeKindWait, domain.NodeKindTerminal,
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
		domain.NodeKindVerify, domain.NodeKindResync, domain.NodeKindToolCall,
		domain.NodeKindWait, domain.NodeKindTerminal,
	} {
		handlers[k] = h
	}
	return workflow.NewRunner(handlers, seededDefStore(), "default")
}

type fakeDispatcher struct {
	result domain.CommandResult
}

func (f fakeDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	ch := make(chan domain.CommandResult, 1)
	res := f.result
	res.CommandID = cmd.ID
	if len(res.Raw) == 0 && cmd.Kind == domain.CommandKindObserve {
		res.Success = true
		res.Raw = json.RawMessage(`{"snapshot":"ready"}`)
	}
	ch <- res
	close(ch)
	return ch, nil
}

func (fakeDispatcher) DeliverResponse(domain.CommandResult) {}

func newRealRunner() *workflow.Runner {
	return newRealRunnerWithTools(toolcatalog.NewLocalToolRegistry())
}

type fakeModelClient struct {
	result json.RawMessage
	err    error
	calls  int
}

func (f *fakeModelClient) GenerateJSON(_ context.Context, _ toolcatalog.JSONModelRequest) (json.RawMessage, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func newRealRunnerWithTools(toolRegistry nodes.ToolRegistry) *workflow.Runner {
	disp := fakeDispatcher{
		result: domain.CommandResult{
			Success: true,
			Raw:     json.RawMessage(`{"snapshot":"ready"}`),
		},
	}
	return workflow.NewRunner(map[domain.NodeKind]workflow.NodeHandler{
		domain.NodeKindObserve:  nodes.NewObserveNode(disp),
		domain.NodeKindDecide:   nodes.NewDecideNode(),
		domain.NodeKindAct:      nodes.NewActNode(disp),
		domain.NodeKindVerify:   nodes.NewVerifyNode(),
		domain.NodeKindResync:   nodes.NewResyncNode(disp),
		domain.NodeKindToolCall: nodes.NewToolCallNode(toolRegistry),
		domain.NodeKindWait:     nodes.NewWaitNode(),
		domain.NodeKindTerminal: nodes.NewTerminalNode(),
	}, seededDefStore(), workflow.DefaultWorkflowName)
}

func newOrch(runner *workflow.Runner, tasks store.TaskStore, states store.WorkflowStateStore) *orchestrator.Orchestrator {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return orchestrator.New(tasks, states, runner, log)
}

func newOrchWithEventStore(
	runner *workflow.Runner,
	tasks store.TaskStore,
	states store.WorkflowStateStore,
	events store.EventPlaneStore,
) *orchestrator.Orchestrator {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return orchestrator.New(tasks, states, runner, log, events)
}

type fakeEmittedEventBus struct {
	accepted  []domain.AcceptedEventRecord
	wakeups   []domain.Event
	acceptErr error
	wakeupErr error
}

func (b *fakeEmittedEventBus) PublishAccepted(_ context.Context, record domain.AcceptedEventRecord) error {
	b.accepted = append(b.accepted, record)
	return b.acceptErr
}

func (b *fakeEmittedEventBus) PublishWakeup(_ context.Context, event domain.Event) error {
	b.wakeups = append(b.wakeups, event)
	return b.wakeupErr
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
	// observe success auto-advances through decide back to observe.
	if ws.CurrentNode != domain.NodeKindObserve {
		t.Errorf("expected Observe after internal auto-advance, got %s", ws.CurrentNode)
	}
	if ws.Revision == 0 {
		t.Errorf("expected checkpoint revision to advance, got %d", ws.Revision)
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
	if err := orch.ProcessEvent(ctx, ev3); !errors.Is(err, domain.ErrEventDropped) {
		t.Fatalf("expected dropped error for stale event, got: %v", err)
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

	// Second identical event must be dropped.
	if err := orch.ProcessEvent(ctx, ev); !errors.Is(err, domain.ErrEventDropped) {
		t.Fatalf("expected dropped error for duplicate event, got: %v", err)
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

func TestProcessEvent_LocalIdentityWorkflow_CompletesWithToolResults(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	task := &domain.Task{
		ID:             "task-local-identity",
		Goal:           "generate local identity profile",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-local-identity",
		WorkflowName:   workflow.LocalIdentityProfileWorkflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrch(newRealRunner(), tasks, states)
	event := domain.Event{
		ID:         "ev-local-identity",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	updatedTask, err := tasks.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("get task error: %v", err)
	}
	if updatedTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %s", updatedTask.Status)
	}

	ws, err := states.Get(context.Background(), task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("state not found: %v", err)
	}
	if ws.CurrentNode != domain.NodeKindTerminal {
		t.Fatalf("expected terminal node, got %s", ws.CurrentNode)
	}
	if ws.Artifacts["goal_reached"] != "true" {
		t.Fatalf("expected goal_reached=true, got %q", ws.Artifacts["goal_reached"])
	}
	for _, key := range []string{
		"profile_full_name",
		"profile_email",
		"profile_password",
		"profile_birth_date",
	} {
		if ws.Artifacts[key] == "" {
			t.Fatalf("expected artifact %s to be set", key)
		}
	}
	if ws.Artifacts["tool_result"] != "" || ws.Artifacts["pending_tool"] != "" {
		t.Fatal("expected transient tool artifacts to be cleared")
	}
}

func TestProcessEvent_LocalIdentityWelcomeEmailWorkflow_UsesModelTool(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	task := &domain.Task{
		ID:             "task-welcome-email",
		Goal:           "generate local identity profile and welcome email",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-welcome-email",
		WorkflowName:   workflow.LocalIdentityWelcomeEmailWorkflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	modelClient := &fakeModelClient{
		result: json.RawMessage(`{"subject":"Selamat datang di AutoSDK","body":"Halo Ayu Lestari, akun AutoSDK Anda siap digunakan. Silakan gunakan email ini untuk melanjutkan proses verifikasi dan simpan kredensial Anda dengan aman.","language":"id","tone":"professional_warm"}`),
	}
	registry := toolcatalog.NewCompositeToolRegistry(
		toolcatalog.NewLocalToolRegistry(),
		toolcatalog.NewModelToolRegistry(nil, modelClient),
	)
	orch := newOrch(newRealRunnerWithTools(registry), tasks, states)

	event := domain.Event{
		ID:         "ev-welcome-email",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	ws, err := states.Get(context.Background(), task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("state not found: %v", err)
	}
	if ws.CurrentNode != domain.NodeKindTerminal {
		t.Fatalf("expected terminal node, got %s", ws.CurrentNode)
	}
	if ws.Artifacts["welcome_email_generation_mode"] != "llm" {
		t.Fatalf("expected llm generation mode, got %q", ws.Artifacts["welcome_email_generation_mode"])
	}
	if ws.Artifacts["welcome_email_subject"] == "" || ws.Artifacts["welcome_email_body"] == "" {
		t.Fatal("expected welcome email artifacts to be set")
	}
	if modelClient.calls != 1 {
		t.Fatalf("expected model tool to be called once, got %d", modelClient.calls)
	}
}

func TestProcessEvent_LocalIdentityWelcomeEmailWorkflow_FallsBackWhenModelToolFails(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()

	task := &domain.Task{
		ID:             "task-welcome-email-fallback",
		Goal:           "generate local identity profile and welcome email",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-welcome-email-fallback",
		WorkflowName:   workflow.LocalIdentityWelcomeEmailWorkflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	modelClient := &fakeModelClient{
		err: errors.New("provider unavailable"),
	}
	registry := toolcatalog.NewCompositeToolRegistry(
		toolcatalog.NewLocalToolRegistry(),
		toolcatalog.NewModelToolRegistry(nil, modelClient),
	)
	orch := newOrch(newRealRunnerWithTools(registry), tasks, states)

	event := domain.Event{
		ID:         "ev-welcome-email-fallback",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	updatedTask, err := tasks.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("get task error: %v", err)
	}
	if updatedTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %s", updatedTask.Status)
	}

	ws, err := states.Get(context.Background(), task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("state not found: %v", err)
	}
	if ws.Artifacts["welcome_email_generation_mode"] != "fallback_template" {
		t.Fatalf("expected fallback template mode, got %q", ws.Artifacts["welcome_email_generation_mode"])
	}
	if ws.Artifacts["welcome_email_error"] == "" {
		t.Fatal("expected welcome_email_error to be captured")
	}
	if ws.Artifacts["welcome_email_subject"] == "" || ws.Artifacts["welcome_email_body"] == "" {
		t.Fatal("expected fallback email artifacts to be set")
	}
	if modelClient.calls != 1 {
		t.Fatalf("expected model tool to be called once, got %d", modelClient.calls)
	}
}

func TestProcessEvent_LocalIdentityWorkflow_RecordsToolResultEvents(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()

	task := &domain.Task{
		ID:             "task-local-identity-events",
		Goal:           "generate local identity profile",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-local-identity-events",
		WorkflowName:   workflow.LocalIdentityProfileWorkflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(newRealRunner(), tasks, states, events)
	event := domain.Event{
		ID:         "ev-local-identity-events",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	accepted, err := events.ListAccepted(context.Background())
	if err != nil {
		t.Fatalf("ListAccepted: %v", err)
	}
	toolResults := 0
	for _, record := range accepted {
		if record.Event.Kind == domain.EventKindToolResult {
			toolResults++
		}
	}
	if toolResults != 4 {
		t.Fatalf("expected 4 tool.result events, got %d", toolResults)
	}
}

func TestProcessAcceptedEvent_LocalIdentityWorkflow_ExternalizesToolResults(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()
	bus := &fakeEmittedEventBus{}

	task := &domain.Task{
		ID:             "task-local-identity-bus",
		Goal:           "generate local identity profile",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-local-identity-bus",
		WorkflowName:   workflow.LocalIdentityProfileWorkflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(newRealRunner(), tasks, states, events)
	orch.SetEmittedEventPublisher(bus)

	event := domain.Event{
		ID:         "ev-local-identity-bus",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	if len(bus.accepted) != 1 || len(bus.wakeups) != 1 {
		t.Fatalf("expected first tool.result to be externalized once, got accepted=%d wakeups=%d", len(bus.accepted), len(bus.wakeups))
	}

	ws, err := states.Get(context.Background(), task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("state not found: %v", err)
	}
	if ws.CurrentNode != domain.NodeKindDecide {
		t.Fatalf("expected workflow to pause at decide awaiting externalized tool.result, got %s", ws.CurrentNode)
	}
	if ws.Artifacts["tool_result"] == "" {
		t.Fatal("expected tool_result artifact to remain checkpointed until externalized event is replayed")
	}

	cursor := 0
	for step := 0; step < 8; step++ {
		currentTask, err := tasks.Get(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("Get task: %v", err)
		}
		if currentTask.Status.IsTerminal() {
			break
		}
		if cursor >= len(bus.wakeups) {
			t.Fatalf("workflow stalled after %d steps; wakeups=%d", step, len(bus.wakeups))
		}
		if err := orch.ProcessAcceptedEvent(context.Background(), bus.wakeups[cursor]); err != nil {
			t.Fatalf("ProcessAcceptedEvent error: %v", err)
		}
		cursor++
	}

	finalTask, err := tasks.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get final task: %v", err)
	}
	if finalTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected completed task after replaying externalized tool results, got %s", finalTask.Status)
	}

	finalState, err := states.Get(context.Background(), task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("Get final state: %v", err)
	}
	if finalState.CurrentNode != domain.NodeKindTerminal {
		t.Fatalf("expected terminal state, got %s", finalState.CurrentNode)
	}
	if len(bus.accepted) != 4 || len(bus.wakeups) != 4 {
		t.Fatalf("expected four externalized tool.result events, got accepted=%d wakeups=%d", len(bus.accepted), len(bus.wakeups))
	}
}

func TestProcessAcceptedEvent_LocalIdentityWorkflow_FallsBackInlineWhenEmittedWakeupPublishFails(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()
	bus := &fakeEmittedEventBus{wakeupErr: errors.New("redis unavailable")}

	task := &domain.Task{
		ID:             "task-local-identity-bus-fallback",
		Goal:           "generate local identity profile",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-local-identity-bus-fallback",
		WorkflowName:   workflow.LocalIdentityProfileWorkflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(newRealRunner(), tasks, states, events)
	orch.SetEmittedEventPublisher(bus)

	event := domain.Event{
		ID:         "ev-local-identity-bus-fallback",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	updatedTask, err := tasks.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if updatedTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected completed task after inline fallback, got %s", updatedTask.Status)
	}
	if len(bus.accepted) != 4 {
		t.Fatalf("expected accepted publish attempt for each tool.result, got %d", len(bus.accepted))
	}
	if len(bus.wakeups) != 4 {
		t.Fatalf("expected wakeup publish attempts for each tool.result, got %d", len(bus.wakeups))
	}
}

func TestProcessEvent_NodeFailure_RecordsDeadLetter(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()

	task := &domain.Task{
		ID:             "task-dead-letter",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-dead-letter",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	runner := workflow.NewRunner(map[domain.NodeKind]workflow.NodeHandler{
		domain.NodeKindObserve: &errorNodeHandler{err: errors.New("observe exploded")},
	}, seededDefStore(), workflow.DefaultWorkflowName)
	orch := newOrchWithEventStore(runner, tasks, states, events)

	err := orch.ProcessEvent(context.Background(), domain.Event{
		ID:         "ev-dead-letter",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected process error")
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
}
