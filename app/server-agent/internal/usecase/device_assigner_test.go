package usecase_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

// --- fakes ---

type fakeEventProcessor struct {
	mu     sync.Mutex
	events []domain.Event
	notify chan struct{} // closed/recreated when an event is appended
}

func newFakeEventProcessor() *fakeEventProcessor {
	return &fakeEventProcessor{notify: make(chan struct{})}
}

func (f *fakeEventProcessor) ProcessEvent(_ context.Context, e domain.Event) error {
	f.mu.Lock()
	f.events = append(f.events, e)
	old := f.notify
	f.notify = make(chan struct{})
	f.mu.Unlock()
	close(old) // wake any waiters
	return nil
}

func (f *fakeEventProcessor) waitForEvents(n int, timeout time.Duration) []domain.Event {
	deadline := time.Now().Add(timeout)
	for {
		f.mu.Lock()
		if len(f.events) >= n {
			out := make([]domain.Event, len(f.events))
			copy(out, f.events)
			f.mu.Unlock()
			return out
		}
		ch := f.notify
		f.mu.Unlock()
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case <-ch:
		case <-time.After(remaining):
		}
	}
	f.mu.Lock()
	out := make([]domain.Event, len(f.events))
	copy(out, f.events)
	f.mu.Unlock()
	return out
}

func (f *fakeEventProcessor) snapshot() []domain.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Event, len(f.events))
	copy(out, f.events)
	return out
}

type fakeRegistry struct {
	sessions []*domain.Session
}

func (r *fakeRegistry) Add(_ *domain.Session, _ registry.Sender) error  { return nil }
func (r *fakeRegistry) Remove(_ domain.SessionID)                        {}
func (r *fakeRegistry) GetBySession(_ domain.SessionID) (*domain.Session, registry.Sender, bool) {
	return nil, nil, false
}
func (r *fakeRegistry) GetByDevice(id domain.DeviceID) (*domain.Session, registry.Sender, bool) {
	for _, s := range r.sessions {
		if s.DeviceID == id {
			return s, nil, true
		}
	}
	return nil, nil, false
}
func (r *fakeRegistry) ListAll() []*domain.Session { return r.sessions }

func newTask(id, status string, device domain.DeviceID) *domain.Task {
	return &domain.Task{
		ID:             domain.TaskID(id),
		Goal:           "test",
		Status:         domain.TaskStatus(status),
		AssignedDevice: device,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

func setupAssigner(t *testing.T, sessions []*domain.Session) (
	*usecase.DeviceAssigner,
	*store.MemoryTaskStore,
	*store.MemoryTaskQueue,
	*fakeEventProcessor,
) {
	t.Helper()
	tasks := store.NewMemoryTaskStore()
	queue := store.NewMemoryTaskQueue()
	reg := &fakeRegistry{sessions: sessions}
	proc := newFakeEventProcessor()
	assigner := usecase.NewDeviceAssigner(tasks, queue, reg, proc, newTestLogger())
	return assigner, tasks, queue, proc
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{}))
}

// --- tests ---

func TestTryAssignPendingToDevice_EmptyQueue(t *testing.T) {
	assigner, _, _, proc := setupAssigner(t, nil)
	ctx := context.Background()

	err := assigner.TryAssignPendingToDevice(ctx, "device-A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proc.snapshot()) != 0 {
		t.Errorf("expected no events emitted, got %d", len(proc.snapshot()))
	}
}

func TestTryAssignPendingToDevice_AssignsQueuedTask(t *testing.T) {
	assigner, tasks, queue, proc := setupAssigner(t, nil)
	ctx := context.Background()

	task := newTask("task-1", "pending", "")
	_ = tasks.Save(ctx, task)
	_ = queue.Enqueue(ctx, task.ID)

	if err := assigner.TryAssignPendingToDevice(ctx, "device-A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tasks.Get(ctx, task.ID)
	if got.AssignedDevice != "device-A" {
		t.Errorf("expected device-A, got %q", got.AssignedDevice)
	}
	if got.Status != domain.TaskStatusRunning {
		t.Errorf("expected running, got %q", got.Status)
	}
	// Bootstrap fires in a goroutine; wait briefly for it.
	events := proc.waitForEvents(1, time.Second)
	if len(events) != 1 || events[0].Kind != domain.EventKindAgentOnline {
		t.Errorf("expected one agent.online event, got %+v", events)
	}
}

func TestTryAssignPendingToDevice_SkipsBusyDevice(t *testing.T) {
	assigner, tasks, queue, proc := setupAssigner(t, nil)
	ctx := context.Background()

	// Device already has a running task.
	running := newTask("task-running", "running", "device-A")
	_ = tasks.Save(ctx, running)

	pending := newTask("task-pending", "pending", "")
	_ = tasks.Save(ctx, pending)
	_ = queue.Enqueue(ctx, pending.ID)

	if err := assigner.TryAssignPendingToDevice(ctx, "device-A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tasks.Get(ctx, pending.ID)
	if got.AssignedDevice != "" {
		t.Errorf("task should not have been assigned; got device %q", got.AssignedDevice)
	}
	if n := len(proc.snapshot()); n != 0 {
		t.Errorf("expected no events, got %d", n)
	}
	// Task must still be in the queue.
	snap, _ := queue.Snapshot(ctx)
	if len(snap) != 1 || snap[0] != pending.ID {
		t.Errorf("pending task should remain in queue; snapshot: %v", snap)
	}
}

func TestTryAssignTaskToIdleDevice_NoDevices(t *testing.T) {
	assigner, tasks, queue, proc := setupAssigner(t, nil)
	ctx := context.Background()

	task := newTask("task-1", "pending", "")
	_ = tasks.Save(ctx, task)

	if err := assigner.TryAssignTaskToIdleDevice(ctx, task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proc.snapshot()) != 0 {
		t.Errorf("expected no events when no devices connected")
	}
	snap, _ := queue.Snapshot(ctx)
	if len(snap) != 1 || snap[0] != task.ID {
		t.Errorf("task should be queued; snapshot: %v", snap)
	}
}

func TestTryAssignTaskToIdleDevice_AssignsToIdleDevice(t *testing.T) {
	sessions := []*domain.Session{{DeviceID: "device-A"}}
	assigner, tasks, _, proc := setupAssigner(t, sessions)
	ctx := context.Background()

	task := newTask("task-1", "pending", "")
	_ = tasks.Save(ctx, task)

	if err := assigner.TryAssignTaskToIdleDevice(ctx, task); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tasks.Get(ctx, task.ID)
	if got.AssignedDevice != "device-A" {
		t.Errorf("expected device-A, got %q", got.AssignedDevice)
	}
	// Bootstrap fires in a goroutine; wait briefly for it.
	if events := proc.waitForEvents(1, time.Second); len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestOnDeviceOffline_RequeuesRunningTask(t *testing.T) {
	assigner, tasks, queue, _ := setupAssigner(t, nil)
	ctx := context.Background()

	task := newTask("task-1", "running", "device-A")
	_ = tasks.Save(ctx, task)

	if err := assigner.OnDeviceOffline(ctx, "device-A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := tasks.Get(ctx, task.ID)
	if got.Status != domain.TaskStatusPaused {
		t.Errorf("expected paused, got %q", got.Status)
	}
	if got.AssignedDevice != "" {
		t.Errorf("expected AssignedDevice cleared, got %q", got.AssignedDevice)
	}
	snap, _ := queue.Snapshot(ctx)
	if len(snap) != 1 || snap[0] != task.ID {
		t.Errorf("task should be in queue; snapshot: %v", snap)
	}
}

func TestOnDeviceOffline_SkipsTerminalTasks(t *testing.T) {
	assigner, tasks, queue, _ := setupAssigner(t, nil)
	ctx := context.Background()

	done := newTask("task-done", "completed", "device-A")
	_ = tasks.Save(ctx, done)

	if err := assigner.OnDeviceOffline(ctx, "device-A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snap, _ := queue.Snapshot(ctx)
	if len(snap) != 0 {
		t.Errorf("terminal task should not be queued; snapshot: %v", snap)
	}
}

func TestOnTaskTerminal_AssignsNextTask(t *testing.T) {
	sessions := []*domain.Session{{DeviceID: "device-A"}}
	assigner, tasks, queue, proc := setupAssigner(t, sessions)
	ctx := context.Background()

	// One pending task waiting in queue.
	pending := newTask("task-next", "pending", "")
	_ = tasks.Save(ctx, pending)
	_ = queue.Enqueue(ctx, pending.ID)

	// Simulate a task finishing on device-A.
	terminal := newTask("task-done", "completed", "device-A")
	_ = tasks.Save(ctx, terminal)

	assigner.OnTaskTerminal(ctx, terminal)

	// Give the async goroutine a moment.
	time.Sleep(50 * time.Millisecond)

	got, _ := tasks.Get(ctx, pending.ID)
	if got.AssignedDevice != "device-A" {
		t.Errorf("expected device-A, got %q", got.AssignedDevice)
	}
	if len(proc.snapshot()) == 0 {
		t.Error("expected bootstrap event after terminal assignment")
	}
}

func TestRemoveFromQueue(t *testing.T) {
	assigner, tasks, queue, _ := setupAssigner(t, nil)
	ctx := context.Background()

	task := newTask("task-1", "pending", "")
	_ = tasks.Save(ctx, task)
	_ = queue.Enqueue(ctx, task.ID)

	if err := assigner.RemoveFromQueue(ctx, task.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snap, _ := queue.Snapshot(ctx)
	if len(snap) != 0 {
		t.Errorf("expected empty queue after remove; got: %v", snap)
	}
}
