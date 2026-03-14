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

func TestMemoryTaskStore_List(t *testing.T) {
	s := store.NewMemoryTaskStore()
	ctx := context.Background()

	_ = s.Save(ctx, &domain.Task{ID: "t-a", Status: domain.TaskStatusPending, CreatedAt: time.Now(), UpdatedAt: time.Now()})
	_ = s.Save(ctx, &domain.Task{ID: "t-b", Status: domain.TaskStatusRunning, CreatedAt: time.Now(), UpdatedAt: time.Now()})

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(list))
	}
}

// --- WorkflowStateStore ---

func TestMemoryWorkflowStateStore_SaveAndGet(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	ws := domain.NewWorkflowState("t-1", "dev-1")
	ws.Inputs["key"] = "value"

	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := s.Get(ctx, "t-1", "dev-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Inputs["key"] != "value" {
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
	ws.Inputs["k"] = "original"
	_ = s.Save(ctx, ws)

	ws.Inputs["k"] = "mutated"
	got, _ := s.Get(ctx, "t-2", "dev-2")
	if got.Inputs["k"] != "original" {
		t.Errorf("store returned mutated artifact: %q", got.Inputs["k"])
	}
}

func TestMemoryWorkflowStateStore_ListActiveByDevice(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	active := domain.NewWorkflowState("t-a", "dev-1")   // CurrentStep="" (active)
	terminal := domain.NewWorkflowState("t-b", "dev-1") // will be moved to terminal
	terminal.CurrentStep = "terminal"
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

func TestMemoryWorkflowStateStore_SaveAdvancesRevision(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	ws := domain.NewWorkflowState("t-rev", "dev-rev")
	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if ws.Revision != 1 {
		t.Fatalf("expected revision 1 after first save, got %d", ws.Revision)
	}

	ws.CurrentStep = "decide"
	if err := s.Save(ctx, ws); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if ws.Revision != 2 {
		t.Fatalf("expected revision 2 after second save, got %d", ws.Revision)
	}
}

func TestMemoryWorkflowStateStore_SaveConflict(t *testing.T) {
	s := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	seed := domain.NewWorkflowState("t-conflict", "dev-conflict")
	if err := s.Save(ctx, seed); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	current, err := s.Get(ctx, seed.TaskID, seed.DeviceID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	stale, err := s.Get(ctx, seed.TaskID, seed.DeviceID)
	if err != nil {
		t.Fatalf("second Get: %v", err)
	}

	current.CurrentStep = "decide"
	if err := s.Save(ctx, current); err != nil {
		t.Fatalf("save current: %v", err)
	}

	stale.CurrentStep = "wait"
	err = s.Save(ctx, stale)
	if !errors.Is(err, store.ErrCheckpointConflict) {
		t.Fatalf("expected checkpoint conflict, got %v", err)
	}

	got, err := s.Get(ctx, seed.TaskID, seed.DeviceID)
	if err != nil {
		t.Fatalf("Get after conflict: %v", err)
	}
	if got.Revision != 2 {
		t.Fatalf("expected stored revision 2 after conflict, got %d", got.Revision)
	}
	if got.CurrentStep != "decide" {
		t.Fatalf("expected current step decide after conflict, got %s", got.CurrentStep)
	}
}

func TestMemoryEventPlaneStore_AcceptAndDeadLetter(t *testing.T) {
	s := store.NewMemoryEventPlaneStore()
	ctx := context.Background()

	event := domain.Event{
		ID:         "dev-1:1",
		Kind:       domain.EventKindAppForeground,
		DeviceID:   "dev-1",
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

	status, err = s.Accept(ctx, event)
	if err != nil {
		t.Fatalf("second Accept: %v", err)
	}
	if status != domain.EventAcceptanceStale && status != domain.EventAcceptanceDuplicate {
		t.Fatalf("expected stale or duplicate on second accept, got %s", status)
	}

	if err := s.RecordDeadLetter(ctx, domain.NewDeadLetterRecord(&event, nil, "boom", "test")); err != nil {
		t.Fatalf("RecordDeadLetter: %v", err)
	}

	accepted, err := s.ListAccepted(ctx)
	if err != nil {
		t.Fatalf("ListAccepted: %v", err)
	}
	if len(accepted) != 1 {
		t.Fatalf("expected 1 accepted event, got %d", len(accepted))
	}

	deadLetters, err := s.ListDeadLetters(ctx)
	if err != nil {
		t.Fatalf("ListDeadLetters: %v", err)
	}
	if len(deadLetters) != 1 {
		t.Fatalf("expected 1 dead letter, got %d", len(deadLetters))
	}
}

func TestMemoryEventPlaneStore_QueryAccepted_CursorPagination(t *testing.T) {
	s := store.NewMemoryEventPlaneStore()
	ctx := context.Background()

	for idx, event := range []domain.Event{
		{ID: "mem-query-1", Kind: domain.EventKindAgentOnline, DeviceID: "dev-mem-query"},
		{ID: "mem-query-2", Kind: domain.EventKindScreenChanged, DeviceID: "dev-mem-query", SeqNo: 1},
		{ID: "mem-query-3", Kind: domain.EventKindAccessibilityDisabled, DeviceID: "dev-mem-query", SeqNo: 2},
	} {
		if _, err := s.Accept(ctx, event); err != nil {
			t.Fatalf("Accept(%s): %v", event.ID, err)
		}
		if idx < 2 {
			time.Sleep(2 * time.Millisecond)
		}
	}

	page1, err := s.QueryAccepted(ctx, store.AcceptedEventListQuery{
		Order: store.EventListOrderDesc,
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("QueryAccepted page1: %v", err)
	}
	if !page1.HasMore || page1.NextCursor == "" {
		t.Fatalf("expected next cursor, got %+v", page1)
	}
	if len(page1.Items) != 2 || page1.Items[0].Event.ID != "mem-query-3" || page1.Items[1].Event.ID != "mem-query-2" {
		t.Fatalf("unexpected first page: %#v", page1.Items)
	}

	page2, err := s.QueryAccepted(ctx, store.AcceptedEventListQuery{
		Order:  store.EventListOrderDesc,
		Limit:  2,
		Cursor: page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("QueryAccepted page2: %v", err)
	}
	if page2.HasMore || page2.NextCursor != "" {
		t.Fatalf("expected terminal page, got %+v", page2)
	}
	if len(page2.Items) != 1 || page2.Items[0].Event.ID != "mem-query-1" {
		t.Fatalf("unexpected second page: %#v", page2.Items)
	}
}

func TestMemoryEventPlaneStore_QueryDeadLetters_TimeRange(t *testing.T) {
	s := store.NewMemoryEventPlaneStore()
	ctx := context.Background()
	base := time.Now().UTC().Add(-10 * time.Minute)
	records := []domain.DeadLetterRecord{
		domain.NewDeadLetterRecord(&domain.Event{ID: "mem-dead-a", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-query"}, nil, "first", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "mem-dead-b", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-query"}, nil, "second", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "mem-dead-c", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-query"}, nil, "third", "orchestrator"),
	}
	for idx := range records {
		records[idx].RecordedAt = base.Add(time.Duration(idx) * time.Minute)
		if err := s.RecordDeadLetter(ctx, records[idx]); err != nil {
			t.Fatalf("RecordDeadLetter(%s): %v", records[idx].ID, err)
		}
	}

	page, err := s.QueryDeadLetters(ctx, store.DeadLetterListQuery{
		From:  base.Add(30 * time.Second),
		To:    base.Add(90 * time.Second),
		Order: store.EventListOrderAsc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("QueryDeadLetters: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Reason != "second" {
		t.Fatalf("unexpected dead-letter query page: %#v", page.Items)
	}
}

func TestMemoryCommandOutboxStore_Lifecycle(t *testing.T) {
	s := store.NewMemoryCommandOutboxStore()
	ctx := context.Background()

	cmd := domain.Command{
		ID:       "cmd-1",
		Kind:     domain.CommandKindObserve,
		DeviceID: "dev-1",
		TaskID:   "task-1",
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

	record, err := s.Get(ctx, cmd.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if record.Status != domain.CommandOutboxStatusResponded {
		t.Fatalf("expected responded status, got %s", record.Status)
	}
	if record.LastResult == nil || !record.LastResult.Success {
		t.Fatal("expected successful last result")
	}
}
