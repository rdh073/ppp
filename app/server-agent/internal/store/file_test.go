package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

func TestFileTaskStore_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := store.NewFileTaskStore(dir)
	if err != nil {
		t.Fatalf("NewFileTaskStore: %v", err)
	}
	task := &domain.Task{
		ID:        "task-file-1",
		Goal:      "persist me",
		Status:    domain.TaskStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.Save(ctx, task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reopened, err := store.NewFileTaskStore(dir)
	if err != nil {
		t.Fatalf("reopen task store: %v", err)
	}
	got, err := reopened.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Goal != task.Goal {
		t.Fatalf("expected goal %q, got %q", task.Goal, got.Goal)
	}
}

func TestFileTaskStore_List(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := store.NewFileTaskStore(dir)
	if err != nil {
		t.Fatalf("NewFileTaskStore: %v", err)
	}
	_ = s.Save(ctx, &domain.Task{ID: "task-list-a", Status: domain.TaskStatusPending, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	_ = s.Save(ctx, &domain.Task{ID: "task-list-b", Status: domain.TaskStatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()})

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(list))
	}
}

func TestFileWorkflowStateStore_PersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := store.NewFileWorkflowStateStore(dir)
	if err != nil {
		t.Fatalf("NewFileWorkflowStateStore: %v", err)
	}
	ws := domain.NewWorkflowState("task-file-2", "dev-file-2")
	ws.Artifacts["hello"] = "world"
	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if ws.Revision != 1 {
		t.Fatalf("expected revision 1 after save, got %d", ws.Revision)
	}

	reopened, err := store.NewFileWorkflowStateStore(dir)
	if err != nil {
		t.Fatalf("reopen workflow state store: %v", err)
	}
	got, err := reopened.Get(ctx, ws.TaskID, ws.DeviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Artifacts["hello"] != "world" {
		t.Fatalf("expected persisted artifact, got %#v", got.Artifacts)
	}
	if got.Revision != 1 {
		t.Fatalf("expected persisted revision 1, got %d", got.Revision)
	}
}

func TestFileWorkflowStateStore_SaveConflict(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := store.NewFileWorkflowStateStore(dir)
	if err != nil {
		t.Fatalf("NewFileWorkflowStateStore: %v", err)
	}
	seed := domain.NewWorkflowState("task-file-conflict", "dev-file-conflict")
	if err := s.Save(ctx, seed); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	current, err := s.Get(ctx, seed.TaskID, seed.DeviceID)
	if err != nil {
		t.Fatalf("Get current: %v", err)
	}
	stale, err := s.Get(ctx, seed.TaskID, seed.DeviceID)
	if err != nil {
		t.Fatalf("Get stale: %v", err)
	}

	current.CurrentNode = domain.NodeKindDecide
	if err := s.Save(ctx, current); err != nil {
		t.Fatalf("save current: %v", err)
	}

	stale.CurrentNode = domain.NodeKindWait
	err = s.Save(ctx, stale)
	if !errors.Is(err, store.ErrCheckpointConflict) {
		t.Fatalf("expected checkpoint conflict, got %v", err)
	}

	got, err := s.Get(ctx, seed.TaskID, seed.DeviceID)
	if err != nil {
		t.Fatalf("Get after conflict: %v", err)
	}
	if got.Revision != 2 {
		t.Fatalf("expected revision 2 after conflict, got %d", got.Revision)
	}
	if got.CurrentNode != domain.NodeKindDecide {
		t.Fatalf("expected current node decide after conflict, got %s", got.CurrentNode)
	}
}

func TestFileEventPlaneStore_PersistsAcceptedAndDeadLetters(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := store.NewFileEventPlaneStore(dir)
	if err != nil {
		t.Fatalf("NewFileEventPlaneStore: %v", err)
	}
	event := domain.Event{
		ID:         "dev-file:1",
		Kind:       domain.EventKindAppForeground,
		DeviceID:   "dev-file",
		SeqNo:      1,
		OccurredAt: time.Now(),
		Payload:    json.RawMessage(`{"seqNo":1}`),
	}
	status, err := s.Accept(ctx, event)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if status != domain.EventAcceptanceAccepted {
		t.Fatalf("expected accepted, got %s", status)
	}
	if err := s.RecordDeadLetter(ctx, domain.NewDeadLetterRecord(&event, nil, "boom", "test")); err != nil {
		t.Fatalf("RecordDeadLetter: %v", err)
	}

	reopened, err := store.NewFileEventPlaneStore(dir)
	if err != nil {
		t.Fatalf("reopen event plane store: %v", err)
	}
	accepted, err := reopened.ListAccepted(ctx)
	if err != nil {
		t.Fatalf("ListAccepted: %v", err)
	}
	if len(accepted) != 1 {
		t.Fatalf("expected 1 accepted event, got %d", len(accepted))
	}
	deadLetters, err := reopened.ListDeadLetters(ctx)
	if err != nil {
		t.Fatalf("ListDeadLetters: %v", err)
	}
	if len(deadLetters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(deadLetters))
	}
}

func TestFileCommandOutboxStore_PersistsLifecycleAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := store.NewFileCommandOutboxStore(dir)
	if err != nil {
		t.Fatalf("NewFileCommandOutboxStore: %v", err)
	}
	cmd := domain.Command{
		ID:       "cmd-file-1",
		Kind:     domain.CommandKindObserve,
		DeviceID: "dev-file",
		TaskID:   "task-file",
		Params:   json.RawMessage(`{}`),
		IssuedAt: time.Now(),
	}
	if err := s.SaveIssued(ctx, cmd); err != nil {
		t.Fatalf("SaveIssued: %v", err)
	}
	if err := s.MarkDispatched(ctx, cmd.ID, time.Now()); err != nil {
		t.Fatalf("MarkDispatched: %v", err)
	}
	if err := s.MarkDelivered(ctx, domain.CommandResult{
		CommandID:  cmd.ID,
		DeviceID:   cmd.DeviceID,
		Success:    true,
		Raw:        json.RawMessage(`{"ok":true}`),
		ReceivedAt: time.Now(),
	}); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	reopened, err := store.NewFileCommandOutboxStore(dir)
	if err != nil {
		t.Fatalf("reopen command outbox store: %v", err)
	}
	record, err := reopened.Get(ctx, cmd.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Status != domain.CommandOutboxStatusResponded {
		t.Fatalf("expected responded status, got %s", record.Status)
	}
	if record.LastResult == nil || !record.LastResult.Success {
		t.Fatal("expected persisted successful result")
	}
}
