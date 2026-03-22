package workflowruntime_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/workflowruntime"
)

func TestTaskControlCreateTaskExplicitDeviceStartsRunningAndBootstrapsWorkflow(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	orch := newTaskControlEventProcessor()
	reg := registry.New()
	session := &domain.Session{
		ID:              domain.SessionID("sess-1"),
		DeviceID:        domain.DeviceID("device-1"),
		ConnectedAt:     time.Now(),
		LastHeartbeatAt: time.Now(),
	}
	if err := reg.Add(session, noopSender{}); err != nil {
		t.Fatalf("registry.Add() error = %v", err)
	}

	uc := workflowruntime.NewTaskControl(tasks, states, orch, reg, newTaskControlLogger())
	task, err := uc.CreateTask(context.Background(), workflowruntime.CreateTaskRequest{
		Goal:         "run explicit task",
		DeviceID:     domain.DeviceID("device-1"),
		WorkflowName: "workflow-a",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.Status != domain.TaskStatusRunning {
		t.Fatalf("task status = %q, want running", task.Status)
	}
	if task.AssignedDevice != "device-1" {
		t.Fatalf("assigned device = %q", task.AssignedDevice)
	}

	events := orch.waitForEvents(1, time.Second)
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if got, want := events[0].Kind, domain.EventKindAgentOnline; got != want {
		t.Fatalf("bootstrap event kind = %q, want %q", got, want)
	}
}

func TestTaskControlListTasksEnrichesWorkflowStateAndOutbox(t *testing.T) {
	ctx := context.Background()
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	outbox := store.NewMemoryCommandOutboxStore()
	uc := workflowruntime.NewTaskControl(tasks, states, newTaskControlEventProcessor(), registry.New(), newTaskControlLogger())
	uc.SetCommandOutbox(outbox)

	task := &domain.Task{
		ID:             domain.TaskID("task-123"),
		Goal:           "inspect summary",
		Status:         domain.TaskStatusCompleted,
		AssignedDevice: domain.DeviceID("device-9"),
		WorkflowName:   "wf-summary",
		CreatedAt:      time.Now().Add(-time.Minute),
		UpdatedAt:      time.Now(),
	}
	if err := tasks.Save(ctx, task); err != nil {
		t.Fatalf("tasks.Save() error = %v", err)
	}
	state := &domain.WorkflowState{
		TaskID:      task.ID,
		DeviceID:    task.AssignedDevice,
		CurrentStep: "publish",
		RetryCount:  2,
		Inputs:      map[string]string{"accountId": "acct-77"},
		UpdatedAt:   time.Now(),
	}
	if err := states.Save(ctx, state); err != nil {
		t.Fatalf("states.Save() error = %v", err)
	}
	if err := outbox.SaveIssued(ctx, domain.Command{
		ID:       "cmd-1",
		TaskID:   task.ID,
		DeviceID: task.AssignedDevice,
		Kind:     domain.CommandKindExecute,
		IssuedAt: time.Now().Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("outbox.SaveIssued() error = %v", err)
	}
	if err := outbox.MarkDispatchFailed(ctx, "cmd-1", "network lost", time.Now()); err != nil {
		t.Fatalf("outbox.MarkDispatchFailed() error = %v", err)
	}

	summaries, err := uc.ListTasks(ctx, workflowruntime.ListTaskQuery{
		WorkflowName: "wf-summary",
	})
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries len = %d, want 1", len(summaries))
	}
	summary := summaries[0]
	if summary.CurrentStep != "publish" {
		t.Fatalf("CurrentStep = %q", summary.CurrentStep)
	}
	if summary.RetryCount != 2 {
		t.Fatalf("RetryCount = %d", summary.RetryCount)
	}
	if got := summary.OutputArtifacts["accountId"]; got != "acct-77" {
		t.Fatalf("OutputArtifacts[accountId] = %q", got)
	}
	if summary.LastCommandStatus != domain.CommandOutboxStatusDispatchFailed {
		t.Fatalf("LastCommandStatus = %q", summary.LastCommandStatus)
	}
	if summary.LastCommandError != "network lost" {
		t.Fatalf("LastCommandError = %q", summary.LastCommandError)
	}
}

type taskControlEventProcessor struct {
	events []domain.Event
	notify chan struct{}
}

func newTaskControlEventProcessor() *taskControlEventProcessor {
	return &taskControlEventProcessor{notify: make(chan struct{})}
}

func (f *taskControlEventProcessor) ProcessEvent(_ context.Context, e domain.Event) error {
	f.events = append(f.events, e)
	old := f.notify
	f.notify = make(chan struct{})
	close(old)
	return nil
}

func (f *taskControlEventProcessor) waitForEvents(n int, timeout time.Duration) []domain.Event {
	deadline := time.Now().Add(timeout)
	for {
		if len(f.events) >= n {
			return append([]domain.Event(nil), f.events...)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case <-f.notify:
		case <-time.After(remaining):
		}
	}
	return append([]domain.Event(nil), f.events...)
}

type noopSender struct{}

func (noopSender) SendRequest(id, method string, params any) error { return nil }
func (noopSender) SendSuccess(id string, result any) error         { return nil }
func (noopSender) SendError(id string, code int, message string) error {
	return nil
}
func (noopSender) Close() error { return nil }

func newTaskControlLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
