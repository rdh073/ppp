package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

type replayAcceptedRecorder struct {
	events []domain.Event
	err    error
}

func (r *replayAcceptedRecorder) ReplayAcceptedEvent(_ context.Context, event domain.Event) error {
	r.events = append(r.events, event)
	return r.err
}

type notificationReplayRecorder struct {
	deviceID domain.DeviceID
	method   string
	payload  json.RawMessage
	err      error
}

func (r *notificationReplayRecorder) IngestNotification(
	_ context.Context,
	deviceID domain.DeviceID,
	method string,
	rawParams json.RawMessage,
) error {
	r.deviceID = deviceID
	r.method = method
	r.payload = append(json.RawMessage(nil), rawParams...)
	return r.err
}

func TestEventPlaneControlReplayAccepted_ReplaysStoredEvent(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	event := domain.Event{
		ID:         "dev-accepted:7",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   "dev-accepted",
		SeqNo:      7,
		OccurredAt: time.Now().UTC(),
	}
	if _, err := events.Accept(context.Background(), event); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	replayer := &replayAcceptedRecorder{}
	uc := usecase.NewEventPlaneControl(events, replayer, &notificationReplayRecorder{}, newLog())

	if err := uc.ReplayAccepted(context.Background(), event.ID); err != nil {
		t.Fatalf("ReplayAccepted: %v", err)
	}
	if len(replayer.events) != 1 || replayer.events[0].ID != event.ID {
		t.Fatalf("unexpected replayed events: %#v", replayer.events)
	}
}

func TestEventPlaneControlReplayDeadLetter_IngestionRoutesToNotificationReplay(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	record := domain.NewDeadLetterRecord(&domain.Event{
		Kind:     domain.EventKindAccessibilityDisabled,
		DeviceID: "dev-dead-ingest",
		Payload:  json.RawMessage(`{"seqNo":9,"reason":"interrupted"}`),
	}, json.RawMessage(`{"seqNo":9,"reason":"interrupted"}`), "decode params: boom", "ingestion")
	if err := events.RecordDeadLetter(context.Background(), record); err != nil {
		t.Fatalf("RecordDeadLetter: %v", err)
	}

	notifications := &notificationReplayRecorder{}
	uc := usecase.NewEventPlaneControl(events, &replayAcceptedRecorder{}, notifications, newLog())

	if err := uc.ReplayDeadLetter(context.Background(), record.ID); err != nil {
		t.Fatalf("ReplayDeadLetter: %v", err)
	}
	if notifications.deviceID != "dev-dead-ingest" {
		t.Fatalf("unexpected device replay target: %q", notifications.deviceID)
	}
	if notifications.method != string(domain.EventKindAccessibilityDisabled) {
		t.Fatalf("unexpected replay method: %q", notifications.method)
	}
}

func TestEventPlaneControlReplayDeadLetter_OrchestratorRoutesToAcceptedReplay(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	record := domain.NewDeadLetterRecord(&domain.Event{
		ID:       "tool-result:1",
		Kind:     domain.EventKindToolResult,
		DeviceID: "dev-orch",
		Payload:  json.RawMessage(`{"taskId":"task-1"}`),
	}, nil, "run node decide: boom", "orchestrator")
	if err := events.RecordDeadLetter(context.Background(), record); err != nil {
		t.Fatalf("RecordDeadLetter: %v", err)
	}

	replayer := &replayAcceptedRecorder{}
	uc := usecase.NewEventPlaneControl(events, replayer, &notificationReplayRecorder{}, newLog())

	if err := uc.ReplayDeadLetter(context.Background(), record.ID); err != nil {
		t.Fatalf("ReplayDeadLetter: %v", err)
	}
	if len(replayer.events) != 1 || replayer.events[0].ID != "tool-result:1" {
		t.Fatalf("unexpected accepted-event replay: %#v", replayer.events)
	}
}

func TestEventPlaneControlReplayDeadLetter_UnexpectedMethodIsNotReplayable(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	record := domain.NewDeadLetterRecord(&domain.Event{
		Kind:     domain.EventKind("agent.hello"),
		DeviceID: "dev-bad-method",
		Payload:  json.RawMessage(`{"foo":"bar"}`),
	}, json.RawMessage(`{"foo":"bar"}`), "unexpected method", "ingestion")
	if err := events.RecordDeadLetter(context.Background(), record); err != nil {
		t.Fatalf("RecordDeadLetter: %v", err)
	}

	uc := usecase.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())

	if err := uc.ReplayDeadLetter(context.Background(), record.ID); err == nil {
		t.Fatal("expected replay dead letter to fail for non-device ingestion source")
	}
}

func TestEventPlaneControlListAccepted_FiltersAndPaginates(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	now := time.Now().UTC()
	acceptedEvents := []domain.Event{
		{
			ID:         "accepted-internal-1",
			Kind:       domain.EventKindAgentOnline,
			DeviceID:   "dev-list",
			OccurredAt: now.Add(-4 * time.Minute),
		},
		{
			ID:         "accepted-device-1",
			Kind:       domain.EventKindScreenChanged,
			DeviceID:   "dev-list",
			SeqNo:      1,
			OccurredAt: now.Add(-3 * time.Minute),
		},
		{
			ID:         "accepted-other-device",
			Kind:       domain.EventKindAccessibilityDisabled,
			DeviceID:   "dev-other",
			SeqNo:      1,
			OccurredAt: now.Add(-2 * time.Minute),
		},
		{
			ID:         "accepted-device-2",
			Kind:       domain.EventKindAccessibilityDisabled,
			DeviceID:   "dev-list",
			SeqNo:      2,
			OccurredAt: now.Add(-1 * time.Minute),
		},
	}
	for _, event := range acceptedEvents {
		if _, err := events.Accept(context.Background(), event); err != nil {
			t.Fatalf("Accept(%s): %v", event.ID, err)
		}
	}

	uc := usecase.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page, err := uc.ListAccepted(context.Background(), usecase.AcceptedEventListQuery{
		DeviceID: "dev-list",
		Source:   "device",
		Order:    usecase.EventListOrderDesc,
		Limit:    1,
		Offset:   1,
	})
	if err != nil {
		t.Fatalf("ListAccepted: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected two filtered device events, got %d", page.Total)
	}
	if len(page.Items) != 1 || page.Items[0].Event.ID != "accepted-device-1" {
		t.Fatalf("unexpected page items: %#v", page.Items)
	}
	if page.Offset != 1 || page.Limit != 1 || page.HasMore {
		t.Fatalf("unexpected page window: %+v", page)
	}
}

func TestEventPlaneControlListDeadLetters_FiltersByEventIDAndOrder(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	records := []domain.DeadLetterRecord{
		domain.NewDeadLetterRecord(&domain.Event{
			ID:       "event-a",
			Kind:     domain.EventKindToolResult,
			DeviceID: "dev-dead-list",
		}, nil, "first", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{
			ID:       "event-b",
			Kind:     domain.EventKindAccessibilityDisabled,
			DeviceID: "dev-dead-list",
		}, nil, "second", "ingestion"),
		domain.NewDeadLetterRecord(&domain.Event{
			ID:       "event-a",
			Kind:     domain.EventKindToolResult,
			DeviceID: "dev-dead-list",
		}, nil, "third", "orchestrator"),
	}
	for _, record := range records {
		if err := events.RecordDeadLetter(context.Background(), record); err != nil {
			t.Fatalf("RecordDeadLetter(%s): %v", record.ID, err)
		}
	}

	uc := usecase.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page, err := uc.ListDeadLetters(context.Background(), usecase.DeadLetterListQuery{
		EventID: "event-a",
		Source:  "orchestrator",
		Order:   usecase.EventListOrderAsc,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ListDeadLetters: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected two filtered dead letters, got %d", page.Total)
	}
	if len(page.Items) != 2 || page.Items[0].Reason != "first" || page.Items[1].Reason != "third" {
		t.Fatalf("unexpected dead-letter page: %#v", page.Items)
	}
}

func TestEventPlaneControlListAccepted_InvalidQuery(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	uc := usecase.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())

	_, err := uc.ListAccepted(context.Background(), usecase.AcceptedEventListQuery{
		Limit: usecase.MaxEventListLimit + 1,
	})
	if err == nil {
		t.Fatal("expected invalid query error")
	}
	if !errors.Is(err, usecase.ErrInvalidEventListQuery) {
		t.Fatalf("expected ErrInvalidEventListQuery, got %v", err)
	}
}
