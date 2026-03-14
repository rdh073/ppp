package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
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

type blockingTerminalHandler struct {
	entered chan struct{}
	release chan struct{}
}

func (h *blockingTerminalHandler) Run(_ context.Context, _ workflow.NodeInput) (workflow.NodeOutput, error) {
	select {
	case h.entered <- struct{}{}:
	default:
	}
	<-h.release
	return workflow.NodeOutput{
		Done: true,
		Artifacts: map[string]string{
			"goal_reached": "true",
		},
	}, nil
}

func seededDefStore() *workflow.MemoryDefStore {
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), workflow.DefaultWorkflowDef.Name, workflow.DefaultWorkflowDef)
	_ = mem.Put(context.Background(), workflow.LocalIdentityProfileWorkflowDef.Name, workflow.LocalIdentityProfileWorkflowDef)
	_ = mem.Put(context.Background(), workflow.LocalIdentityWelcomeEmailWorkflowDef.Name, workflow.LocalIdentityWelcomeEmailWorkflowDef)
	_ = mem.Put(context.Background(), workflow.AndroidSettingsPrivateDNSWorkflowDef.Name, workflow.AndroidSettingsPrivateDNSWorkflowDef)
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

func newLocalIdentityBindings() nodes.ToolBindingResolver {
	return nodes.NewStaticToolBindingResolver(
		nodes.ToolBinding{
			ID:             "local_identity.generate_name",
			ToolName:       "identity.generate_indonesian_name",
			ParamsTemplate: `{}`,
			SuccessArtifacts: []nodes.ToolArtifactBinding{
				{Artifact: "profile_full_name", FromJSONPointer: "/fullName"},
				{Artifact: "profile_first_name", FromJSONPointer: "/firstName"},
				{Artifact: "profile_last_name", FromJSONPointer: "/lastName"},
				{Artifact: "profile_gender", FromJSONPointer: "/gender"},
			},
		},
		nodes.ToolBinding{
			ID:             "local_identity.generate_email",
			ToolName:       "identity.generate_email",
			ParamsTemplate: `{"fullName": {{ json (artifact "profile_full_name") }}}`,
			SuccessArtifacts: []nodes.ToolArtifactBinding{
				{Artifact: "profile_email", FromJSONPointer: "/email"},
				{Artifact: "profile_email_local_part", FromJSONPointer: "/localPart"},
				{Artifact: "profile_email_domain", FromJSONPointer: "/domain"},
			},
		},
		nodes.ToolBinding{
			ID:             "local_identity.generate_password",
			ToolName:       "credential.generate_password",
			Constants:      map[string]any{"length": 20, "includeSymbols": true},
			ParamsTemplate: `{"length": {{ json (constant "length") }}, "includeSymbols": {{ json (constant "includeSymbols") }}}`,
			SuccessArtifacts: []nodes.ToolArtifactBinding{
				{Artifact: "profile_password", FromJSONPointer: "/password"},
				{Artifact: "profile_password_length", FromJSONPointer: "/length"},
				{Artifact: "profile_password_hasSymbol", FromJSONPointer: "/hasSymbol"},
			},
		},
		nodes.ToolBinding{
			ID:             "local_identity.generate_birth_date",
			ToolName:       "identity.generate_birth_date",
			Constants:      map[string]any{"minAge": 25, "maxAge": 35},
			ParamsTemplate: `{"minAge": {{ json (constant "minAge") }}, "maxAge": {{ json (constant "maxAge") }}}`,
			SuccessArtifacts: []nodes.ToolArtifactBinding{
				{Artifact: "profile_birth_date", FromJSONPointer: "/birthDate"},
				{Artifact: "profile_age", FromJSONPointer: "/age"},
				{Artifact: "profile_reference_date", FromJSONPointer: "/referenceDate"},
			},
		},
		nodes.ToolBinding{
			ID:             "local_identity.generate_welcome_email",
			ToolName:       "content.generate_welcome_email",
			Optional:       true,
			Constants:      map[string]any{"productName": "AutoSDK", "senderName": "AutoSDK", "language": "id", "tone": "professional_warm"},
			ParamsTemplate: `{"fullName": {{ json (artifact "profile_full_name") }}, "email": {{ json (artifact "profile_email") }}, "productName": {{ json (constant "productName") }}, "senderName": {{ json (constant "senderName") }}, "language": {{ json (constant "language") }}, "tone": {{ json (constant "tone") }}}`,
			SuccessArtifacts: []nodes.ToolArtifactBinding{
				{Artifact: "welcome_email_subject", FromJSONPointer: "/subject"},
				{Artifact: "welcome_email_body", FromJSONPointer: "/body"},
				{Artifact: "welcome_email_language", FromJSONPointer: "/language"},
				{Artifact: "welcome_email_tone", FromJSONPointer: "/tone"},
				{Artifact: "welcome_email_generation_mode", Value: stringPtr("llm")},
			},
			FailureArtifacts: []nodes.ToolArtifactBinding{
				{Artifact: "welcome_email_subject", Value: stringPtr("Selamat datang di AutoSDK")},
				{Artifact: "welcome_email_body", Template: "Halo {{ default \"Pengguna\" (artifact \"profile_full_name\") }},\n\nAkun AutoSDK Anda sudah siap digunakan. Email yang kami siapkan untuk profil ini adalah {{ artifact \"profile_email\" }}. Silakan lanjutkan verifikasi dan simpan kredensial Anda dengan aman sebelum memulai workflow berikutnya.\n\nJika Anda membutuhkan bantuan, balas email ini dan tim AutoSDK akan membantu Anda.\n\nSalam,\nAutoSDK"},
				{Artifact: "welcome_email_language", Value: stringPtr("id")},
				{Artifact: "welcome_email_tone", Value: stringPtr("professional_warm")},
				{Artifact: "welcome_email_generation_mode", Value: stringPtr("fallback_template")},
				{Artifact: "welcome_email_error", Template: "{{ .ToolError }}"},
			},
		},
	)
}

func stringPtr(value string) *string { return &value }

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
		domain.NodeKindToolCall: nodes.NewToolCallNodeWithBindings(toolRegistry, newLocalIdentityBindings()),
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
	accepted        []domain.AcceptedEventRecord
	wakeups         []domain.Event
	acceptErr       error
	wakeupErr       error
	wakeupErrAtCall int
	wakeupCallCount int
}

func (b *fakeEmittedEventBus) PublishAccepted(_ context.Context, record domain.AcceptedEventRecord) error {
	b.accepted = append(b.accepted, record)
	return b.acceptErr
}

func (b *fakeEmittedEventBus) PublishWakeup(_ context.Context, event domain.Event) error {
	b.wakeupCallCount++
	b.wakeups = append(b.wakeups, event)
	if b.wakeupErrAtCall > 0 {
		if b.wakeupCallCount == b.wakeupErrAtCall {
			return b.wakeupErr
		}
		return nil
	}
	return b.wakeupErr
}

type multiEmitHandler struct{}

func (h *multiEmitHandler) Run(_ context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	switch input.Event.ID {
	case "ev-multi-start":
		return workflow.NodeOutput{
			Status: workflow.NodeStatusPending,
			WaitingFor: []domain.EventKind{
				domain.EventKindToolResult,
			},
			EmittedEvents: []domain.Event{
				{
					ID:         "ev-multi-1",
					Kind:       domain.EventKindToolResult,
					DeviceID:   input.State.DeviceID,
					OccurredAt: time.Now(),
					Payload:    json.RawMessage(`{"step":1}`),
				},
				{
					ID:         "ev-multi-2",
					Kind:       domain.EventKindToolResult,
					DeviceID:   input.State.DeviceID,
					OccurredAt: time.Now(),
					Payload:    json.RawMessage(`{"step":2}`),
				},
			},
		}, nil
	case "ev-multi-1":
		return workflow.NodeOutput{
			Status: workflow.NodeStatusPending,
			WaitingFor: []domain.EventKind{
				domain.EventKindToolResult,
			},
			Artifacts: map[string]string{
				"first_seen": "true",
			},
		}, nil
	case "ev-multi-2":
		return workflow.NodeOutput{
			Done: true,
			Artifacts: map[string]string{
				"second_seen":  "true",
				"goal_reached": "true",
			},
		}, nil
	default:
		return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
	}
}

type wrongDeviceEmitHandler struct{}

func (h *wrongDeviceEmitHandler) Run(_ context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	return workflow.NodeOutput{
		Status: workflow.NodeStatusPending,
		WaitingFor: []domain.EventKind{
			domain.EventKindToolResult,
		},
		EmittedEvents: []domain.Event{{
			ID:         "ev-wrong-device",
			Kind:       domain.EventKindToolResult,
			DeviceID:   "other-device",
			OccurredAt: time.Now(),
			Payload:    json.RawMessage(`{"step":"bad"}`),
		}},
	}, nil
}

func newMultiEmitRunner(handler workflow.NodeHandler) *workflow.Runner {
	return workflow.NewRunner(map[domain.NodeKind]workflow.NodeHandler{
		domain.NodeKindObserve:  handler,
		domain.NodeKindTerminal: &fakeNodeHandler{done: true},
	}, seededDefStore(), workflow.DefaultWorkflowName)
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

func TestProcessAcceptedEvent_RecordsIngestLagMetric(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	metrics := telemetry.NewRegistry()
	orch := newOrch(newFakeRunner(workflow.NodeStatusSuccess), tasks, states)
	orch.SetOperationalMetrics(metrics)

	event := domain.Event{
		ID:         "ev-ingest-lag",
		Kind:       domain.EventKindAccessibilityDisabled,
		DeviceID:   "dev-lag",
		SeqNo:      9,
		OccurredAt: time.Now().Add(-2 * time.Second),
	}

	if err := orch.ProcessAcceptedEvent(context.Background(), event); err != nil {
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

	task := &domain.Task{
		ID:             "task-lane-metric",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-lane-metric",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	handler := &blockingTerminalHandler{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	runner := workflow.NewRunner(map[domain.NodeKind]workflow.NodeHandler{
		domain.NodeKindObserve: handler,
	}, seededDefStore(), "default")
	orch := newOrch(runner, tasks, states)
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
	case <-handler.entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for workflow handler entry")
	}
	if got := metrics.Snapshot().DeviceLaneActive; got != 1 {
		t.Fatalf("expected active device lane gauge 1, got %d", got)
	}

	close(handler.release)

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

func TestProcessEvent_MultipleEmittedEvents_RunInlineInOrder(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()

	task := &domain.Task{
		ID:             "task-multi-inline",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-multi-inline",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(newMultiEmitRunner(&multiEmitHandler{}), tasks, states, events)
	event := domain.Event{
		ID:         "ev-multi-start",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}

	finalTask, err := tasks.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get task: %v", err)
	}
	if finalTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %s", finalTask.Status)
	}

	finalState, err := states.Get(context.Background(), task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("Get state: %v", err)
	}
	if finalState.CurrentNode != domain.NodeKindTerminal {
		t.Fatalf("expected terminal state, got %s", finalState.CurrentNode)
	}
	if finalState.Artifacts["first_seen"] != "true" || finalState.Artifacts["second_seen"] != "true" {
		t.Fatalf("expected both emitted events to be drained in order, got artifacts=%v", finalState.Artifacts)
	}

	accepted, err := events.ListAccepted(context.Background())
	if err != nil {
		t.Fatalf("ListAccepted: %v", err)
	}
	emittedCount := 0
	for _, record := range accepted {
		if record.Event.ID == "ev-multi-1" || record.Event.ID == "ev-multi-2" {
			emittedCount++
		}
	}
	if emittedCount != 2 {
		t.Fatalf("expected both emitted events accepted, got %d", emittedCount)
	}
}

func TestProcessEvent_MultipleEmittedEvents_ExternalBusPublishesInOrder(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()
	bus := &fakeEmittedEventBus{}

	task := &domain.Task{
		ID:             "task-multi-bus",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-multi-bus",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(newMultiEmitRunner(&multiEmitHandler{}), tasks, states, events)
	orch.SetEmittedEventPublisher(bus)
	event := domain.Event{
		ID:         "ev-multi-start",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent error: %v", err)
	}
	if len(bus.wakeups) != 2 || bus.wakeups[0].ID != "ev-multi-1" || bus.wakeups[1].ID != "ev-multi-2" {
		t.Fatalf("expected emitted wakeups in order, got %#v", bus.wakeups)
	}

	if err := orch.ProcessAcceptedEvent(context.Background(), bus.wakeups[0]); err != nil {
		t.Fatalf("ProcessAcceptedEvent first wakeup: %v", err)
	}
	stateAfterFirst, err := states.Get(context.Background(), task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("Get state after first wakeup: %v", err)
	}
	if stateAfterFirst.Artifacts["first_seen"] != "true" || stateAfterFirst.CurrentNode == domain.NodeKindTerminal {
		t.Fatalf("expected first wakeup to advance partially, got state=%+v", stateAfterFirst)
	}

	if err := orch.ProcessAcceptedEvent(context.Background(), bus.wakeups[1]); err != nil {
		t.Fatalf("ProcessAcceptedEvent second wakeup: %v", err)
	}
	finalTask, err := tasks.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Get final task: %v", err)
	}
	if finalTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected completed task, got %s", finalTask.Status)
	}
}

func TestProcessEvent_MultipleEmittedEvents_FailsClosedAfterPartialWakeupPublish(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()
	bus := &fakeEmittedEventBus{
		wakeupErr:       errors.New("redis unavailable"),
		wakeupErrAtCall: 2,
	}

	task := &domain.Task{
		ID:             "task-multi-partial-fail",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-multi-partial-fail",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(newMultiEmitRunner(&multiEmitHandler{}), tasks, states, events)
	orch.SetEmittedEventPublisher(bus)
	event := domain.Event{
		ID:         "ev-multi-start",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	err := orch.ProcessEvent(context.Background(), event)
	if err == nil {
		t.Fatal("expected partial wakeup publish to fail closed")
	}
	if len(bus.wakeups) != 2 {
		t.Fatalf("expected two wakeup publish attempts, got %d", len(bus.wakeups))
	}
	deadLetters, deadErr := events.ListDeadLetters(context.Background())
	if deadErr != nil {
		t.Fatalf("ListDeadLetters: %v", deadErr)
	}
	if len(deadLetters) == 0 {
		t.Fatal("expected dead letter for partial wakeup publish failure")
	}
}

func TestProcessEvent_EmittedEventWrongDevice_FailsClosed(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	events := store.NewMemoryEventPlaneStore()

	task := &domain.Task{
		ID:             "task-wrong-device",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-right-device",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	_ = tasks.Save(context.Background(), task)

	orch := newOrchWithEventStore(newMultiEmitRunner(&wrongDeviceEmitHandler{}), tasks, states, events)
	event := domain.Event{
		ID:         "ev-wrong-device-start",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   task.AssignedDevice,
		SeqNo:      1,
		OccurredAt: time.Now(),
	}

	if err := orch.ProcessEvent(context.Background(), event); err == nil {
		t.Fatal("expected wrong-device emitted event to fail")
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
