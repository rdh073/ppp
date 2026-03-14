package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// --- TaskStore ---

func TestMemoryTaskStore_SaveAndGet(t *testing.T) {
	s := store.NewMemoryTaskStore()
	ctx := context.Background()

	task := &domain.Task{
		ID:        "t-1",
		Goal:      "do something",
		Status:    domain.TaskStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.Save(ctx, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(ctx, "t-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Goal != task.Goal {
		t.Errorf("goal mismatch: want %q got %q", task.Goal, got.Goal)
	}
}

func TestMemoryTaskStore_GetNotFound(t *testing.T) {
	s := store.NewMemoryTaskStore()
	_, err := s.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestMemoryTaskStore_SaveIsolatesCallerMutation(t *testing.T) {
	s := store.NewMemoryTaskStore()
	ctx := context.Background()

	task := &domain.Task{ID: "t-2", Goal: "original", Status: domain.TaskStatusPending, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	_ = s.Save(ctx, task)

	task.Goal = "mutated"
	got, _ := s.Get(ctx, "t-2")
	if got.Goal != "original" {
		t.Errorf("store returned mutated value: %q", got.Goal)
	}
}

func TestMemoryTaskStore_ListByDevice(t *testing.T) {
	s := store.NewMemoryTaskStore()
	ctx := context.Background()

	_ = s.Save(ctx, &domain.Task{ID: "t-a", AssignedDevice: "dev-1", Status: domain.TaskStatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	_ = s.Save(ctx, &domain.Task{ID: "t-b", AssignedDevice: "dev-1", Status: domain.TaskStatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	_ = s.Save(ctx, &domain.Task{ID: "t-c", AssignedDevice: "dev-2", Status: domain.TaskStatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()})

	list, err := s.ListByDevice(ctx, "dev-1")
	if err != nil {
		t.Fatalf("ListByDevice: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 tasks for dev-1, got %d", len(list))
	}
}

// --- WorkflowStateStore ---

func TestMemoryWorkflowStateStore_SaveAndGet(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	ws := domain.NewWorkflowState("t-1", "dev-1")
	ws.Artifacts["key"] = "value"

	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(ctx, "t-1", "dev-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Artifacts["key"] != "value" {
		t.Errorf("artifact not stored")
	}
}

func TestMemoryWorkflowStateStore_GetNotFound(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	_, err := s.Get(context.Background(), "t-x", "dev-x")
	if err == nil {
		t.Fatal("expected error for missing state")
	}
}

func TestMemoryWorkflowStateStore_SaveIsolatesCallerMutation(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	ws := domain.NewWorkflowState("t-2", "dev-2")
	ws.Artifacts["k"] = "original"
	_ = s.Save(ctx, ws)

	ws.Artifacts["k"] = "mutated"
	got, _ := s.Get(ctx, "t-2", "dev-2")
	if got.Artifacts["k"] != "original" {
		t.Errorf("store returned mutated artifact: %q", got.Artifacts["k"])
	}
}

func TestMemoryWorkflowStateStore_ListActiveByDevice(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	active := domain.NewWorkflowState("t-a", "dev-1")  // CurrentNode=Observe (active)
	terminal := domain.NewWorkflowState("t-b", "dev-1") // will be moved to Terminal
	terminal.CurrentNode = domain.NodeKindTerminal
	other := domain.NewWorkflowState("t-c", "dev-2") // different device

	_ = s.Save(ctx, active)
	_ = s.Save(ctx, terminal)
	_ = s.Save(ctx, other)

	list, err := s.ListActiveByDevice(ctx, "dev-1")
	if err != nil {
		t.Fatalf("ListActiveByDevice: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 active state for dev-1, got %d", len(list))
	}
	if list[0].TaskID != "t-a" {
		t.Errorf("wrong task returned: %s", list[0].TaskID)
	}
}
