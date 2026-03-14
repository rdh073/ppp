package usecase_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// --- shared fakes ---

type noopSender struct{}

func (noopSender) SendRequest(_, _ string, _ any) error      { return nil }
func (noopSender) SendSuccess(_ string, _ any) error         { return nil }
func (noopSender) SendError(_ string, _ int, _ string) error { return nil }
func (noopSender) Close() error                              { return nil }

type recordingProcessor struct {
	events []domain.Event
}

func (r *recordingProcessor) ProcessEvent(_ context.Context, e domain.Event) error {
	r.events = append(r.events, e)
	return nil
}

type pendingNode struct{}

func (pendingNode) Run(_ context.Context, _ workflow.NodeInput) (workflow.NodeOutput, error) {
	return workflow.NodeOutput{
		Status:     workflow.NodeStatusPending,
		WaitingFor: []domain.EventKind{domain.EventKindAgentOffline},
	}, nil
}

func newLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- AgentLifecycleUseCase ---

func TestHello_RegistersSessionAndFiresOnlineEvent(t *testing.T) {
	reg := registry.New()
	proc := &recordingProcessor{}
	uc := usecase.NewAgentLifecycle(reg, proc, newLog())

	resp, err := uc.Hello(context.Background(), usecase.HelloRequest{
		DeviceID:        "dev-1",
		AgentInstanceID: "inst-1",
		Capabilities:    []domain.Capability{{Name: "observe"}},
	}, noopSender{})
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}

	// Session must be registered.
	_, _, ok := reg.GetByDevice("dev-1")
	if !ok {
		t.Error("session not found by device after Hello")
	}
	// SessionID must be returned.
	if resp.SessionID == "" {
		t.Error("empty SessionID in response")
	}
	// Orchestrator must have received AgentOnline event.
	if len(proc.events) != 1 || proc.events[0].Kind != domain.EventKindAgentOnline {
		t.Errorf("expected one AgentOnline event, got %v", proc.events)
	}
}

func TestHello_ReplacesExistingSession(t *testing.T) {
	reg := registry.New()
	proc := &recordingProcessor{}
	uc := usecase.NewAgentLifecycle(reg, proc, newLog())

	// First hello.
	resp1, _ := uc.Hello(context.Background(), usecase.HelloRequest{DeviceID: "dev-2"}, noopSender{})
	// Second hello with same device.
	resp2, _ := uc.Hello(context.Background(), usecase.HelloRequest{DeviceID: "dev-2"}, noopSender{})

	if resp1.SessionID == resp2.SessionID {
		t.Error("second Hello should assign a new SessionID")
	}
	// Only the new session should exist.
	s, _, _ := reg.GetByDevice("dev-2")
	if s.ID != resp2.SessionID {
		t.Errorf("registry has wrong session: want %s got %s", resp2.SessionID, s.ID)
	}
}

func TestResume_UnknownSession_Error(t *testing.T) {
	reg := registry.New()
	uc := usecase.NewAgentLifecycle(reg, &recordingProcessor{}, newLog())

	_, err := uc.Resume(context.Background(), usecase.ResumeRequest{
		SessionID: "sess-nonexistent",
		DeviceID:  "dev-x",
	}, noopSender{})
	if err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestResume_KnownSession_Accepted(t *testing.T) {
	reg := registry.New()
	proc := &recordingProcessor{}
	uc := usecase.NewAgentLifecycle(reg, proc, newLog())

	// First, register via Hello.
	resp, _ := uc.Hello(context.Background(), usecase.HelloRequest{DeviceID: "dev-3"}, noopSender{})
	proc.events = nil // reset

	// Resume with the issued sessionID.
	resumeResp, err := uc.Resume(context.Background(), usecase.ResumeRequest{
		SessionID: resp.SessionID,
		DeviceID:  "dev-3",
	}, noopSender{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumeResp.SessionID != resp.SessionID {
		t.Errorf("expected same sessionID on resume")
	}
	if len(proc.events) != 1 || proc.events[0].Kind != domain.EventKindAgentOnline {
		t.Errorf("expected AgentOnline on resume, got %v", proc.events)
	}
}

func TestDisconnect_RemovesSessionAndFiresOfflineEvent(t *testing.T) {
	reg := registry.New()
	proc := &recordingProcessor{}
	uc := usecase.NewAgentLifecycle(reg, proc, newLog())

	resp, _ := uc.Hello(context.Background(), usecase.HelloRequest{DeviceID: "dev-4"}, noopSender{})
	proc.events = nil

	uc.Disconnect(context.Background(), "dev-4", resp.SessionID)

	_, _, ok := reg.GetByDevice("dev-4")
	if ok {
		t.Error("session should be removed after Disconnect")
	}
	if len(proc.events) != 1 || proc.events[0].Kind != domain.EventKindAgentOffline {
		t.Errorf("expected AgentOffline event, got %v", proc.events)
	}
}

// --- TaskControlUseCase ---

func newTaskUC(reg registry.AgentRegistry) (*usecase.TaskControlUseCase, *store.MemoryTaskStore) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	proc := &recordingProcessor{}
	return usecase.NewTaskControl(tasks, states, proc, reg, newLog()), tasks
}

func TestCreateTask_NoDevice_Pending(t *testing.T) {
	reg := registry.New()
	uc, tasks := newTaskUC(reg)

	task, err := uc.CreateTask(context.Background(), usecase.CreateTaskRequest{
		Goal:           "do work",
		InputArtifacts: map[string]string{"account.email": "ada@example.com"},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != domain.TaskStatusPending {
		t.Errorf("expected Pending, got %s", task.Status)
	}
	if task.InputArtifacts["account.email"] != "ada@example.com" {
		t.Fatalf("expected task inputArtifacts to be returned, got %#v", task.InputArtifacts)
	}

	task.InputArtifacts["account.email"] = "mutated@example.com"

	stored, _ := tasks.Get(context.Background(), task.ID)
	if stored.Goal != "do work" {
		t.Errorf("goal not stored")
	}
	if stored.InputArtifacts["account.email"] != "ada@example.com" {
		t.Fatalf("expected stored inputArtifacts to be isolated from caller mutation, got %#v", stored.InputArtifacts)
	}
}

func TestCreateTask_WithConnectedDevice_Running(t *testing.T) {
	reg := registry.New()
	// Pre-register a session for dev-5.
	sess := &domain.Session{ID: "sess-5", DeviceID: "dev-5", ConnectedAt: time.Now(), LastHeartbeatAt: time.Now()}
	_ = reg.Add(sess, noopSender{})

	uc, _ := newTaskUC(reg)

	task, err := uc.CreateTask(context.Background(), usecase.CreateTaskRequest{
		Goal:     "run on device",
		DeviceID: "dev-5",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Status != domain.TaskStatusRunning {
		t.Errorf("expected Running when device connected, got %s", task.Status)
	}
	if task.AssignedDevice != "dev-5" {
		t.Errorf("device not assigned")
	}
}

func TestCreateTask_WithConnectedDevice_BootstrapsWorkflowStateWithInputArtifacts(t *testing.T) {
	ctx := context.Background()
	reg := registry.New()
	sess := &domain.Session{ID: "sess-bootstrap", DeviceID: "dev-bootstrap", ConnectedAt: time.Now(), LastHeartbeatAt: time.Now()}
	_ = reg.Add(sess, noopSender{})

	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	runner := workflow.NewRunner(
		map[domain.NodeKind]workflow.NodeHandler{
			domain.NodeKindObserve: pendingNode{},
		},
		workflow.NewMemoryDefStore(),
		workflow.DefaultWorkflowName,
	)
	orch := orchestrator.New(tasks, states, runner, newLog())
	uc := usecase.NewTaskControl(tasks, states, orch, reg, newLog())

	task, err := uc.CreateTask(ctx, usecase.CreateTaskRequest{
		Goal:           "run on device",
		DeviceID:       "dev-bootstrap",
		InputArtifacts: map[string]string{"account.email": "ada@example.com", "ticket.id": "42"},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	state, err := states.Get(ctx, task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("Get state: %v", err)
	}
	if state.Artifacts["account.email"] != "ada@example.com" || state.Artifacts["ticket.id"] != "42" {
		t.Fatalf("expected bootstrapped inputArtifacts in workflow state, got %#v", state.Artifacts)
	}
	if state.Revision != 1 {
		t.Fatalf("expected bootstrapped state revision 1, got %d", state.Revision)
	}
}

func TestCreateTask_WithUnconnectedDevice_Error(t *testing.T) {
	reg := registry.New()
	uc, _ := newTaskUC(reg)

	_, err := uc.CreateTask(context.Background(), usecase.CreateTaskRequest{
		Goal:     "will fail",
		DeviceID: "dev-offline",
	})
	if err == nil {
		t.Fatal("expected error when device not connected")
	}
}

func TestCreateTask_EmptyGoal_Error(t *testing.T) {
	reg := registry.New()
	uc, _ := newTaskUC(reg)

	_, err := uc.CreateTask(context.Background(), usecase.CreateTaskRequest{Goal: ""})
	if err == nil {
		t.Fatal("expected error for empty goal")
	}
}

func TestCancelTask_Running_Cancelled(t *testing.T) {
	reg := registry.New()
	uc, tasks := newTaskUC(reg)

	task, _ := uc.CreateTask(context.Background(), usecase.CreateTaskRequest{Goal: "cancel me"})
	if err := uc.CancelTask(context.Background(), task.ID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}

	stored, _ := tasks.Get(context.Background(), task.ID)
	if stored.Status != domain.TaskStatusCancelled {
		t.Errorf("expected Cancelled, got %s", stored.Status)
	}
}

func TestCancelTask_AlreadyTerminal_Error(t *testing.T) {
	reg := registry.New()
	uc, tasks := newTaskUC(reg)

	task, _ := uc.CreateTask(context.Background(), usecase.CreateTaskRequest{Goal: "done task"})
	// Manually mark terminal.
	task.Status = domain.TaskStatusCompleted
	_ = tasks.Save(context.Background(), task)

	err := uc.CancelTask(context.Background(), task.ID)
	if err == nil {
		t.Fatal("expected error cancelling already-terminal task")
	}
}

func TestGetTask_Found(t *testing.T) {
	reg := registry.New()
	uc, _ := newTaskUC(reg)

	task, _ := uc.CreateTask(context.Background(), usecase.CreateTaskRequest{Goal: "get me"})

	got, err := uc.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != task.ID {
		t.Errorf("wrong task returned")
	}
}

func TestGetTask_NotFound_Error(t *testing.T) {
	reg := registry.New()
	uc, _ := newTaskUC(reg)

	_, err := uc.GetTask(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}
