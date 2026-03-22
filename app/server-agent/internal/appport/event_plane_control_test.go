package appport_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/eventing"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
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
	metrics := telemetry.NewRegistry()
	uc := eventing.NewEventPlaneControl(events, replayer, &notificationReplayRecorder{}, newLog(), metrics)

	if err := uc.ReplayAccepted(context.Background(), event.ID); err != nil {
		t.Fatalf("ReplayAccepted: %v", err)
	}
	if len(replayer.events) != 1 || replayer.events[0].ID != event.ID {
		t.Fatalf("unexpected replayed events: %#v", replayer.events)
	}
	if got := metrics.Snapshot().Replay[telemetry.ReplayPathAccepted][telemetry.ReplayOutcomeSucceeded]; got != 1 {
		t.Fatalf("expected accepted replay success metric 1, got %d", got)
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
	metrics := telemetry.NewRegistry()
	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, notifications, newLog(), metrics)

	if err := uc.ReplayDeadLetter(context.Background(), record.ID); err != nil {
		t.Fatalf("ReplayDeadLetter: %v", err)
	}
	if notifications.deviceID != "dev-dead-ingest" {
		t.Fatalf("unexpected device replay target: %q", notifications.deviceID)
	}
	if notifications.method != string(domain.EventKindAccessibilityDisabled) {
		t.Fatalf("unexpected replay method: %q", notifications.method)
	}
	if got := metrics.Snapshot().Replay[telemetry.ReplayPathDeadLetterIngestion][telemetry.ReplayOutcomeSucceeded]; got != 1 {
		t.Fatalf("expected dead-letter ingestion replay success metric 1, got %d", got)
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
	metrics := telemetry.NewRegistry()
	uc := eventing.NewEventPlaneControl(events, replayer, &notificationReplayRecorder{}, newLog(), metrics)

	if err := uc.ReplayDeadLetter(context.Background(), record.ID); err != nil {
		t.Fatalf("ReplayDeadLetter: %v", err)
	}
	if len(replayer.events) != 1 || replayer.events[0].ID != "tool-result:1" {
		t.Fatalf("unexpected accepted-event replay: %#v", replayer.events)
	}
	if got := metrics.Snapshot().Replay[telemetry.ReplayPathDeadLetterAccepted][telemetry.ReplayOutcomeSucceeded]; got != 1 {
		t.Fatalf("expected dead-letter accepted replay success metric 1, got %d", got)
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

	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())

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

	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		DeviceID: "dev-list",
		Source:   "device",
		Order:    eventing.EventListOrderDesc,
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

func TestEventPlaneControlListAccepted_FiltersByAcceptedTimeRange(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	acceptedEvents := []domain.Event{
		{ID: "accepted-time-1", Kind: domain.EventKindAgentOnline, DeviceID: "dev-time-range"},
		{ID: "accepted-time-2", Kind: domain.EventKindScreenChanged, DeviceID: "dev-time-range", SeqNo: 1},
		{ID: "accepted-time-3", Kind: domain.EventKindAccessibilityDisabled, DeviceID: "dev-time-range", SeqNo: 2},
	}
	for idx, event := range acceptedEvents {
		if _, err := events.Accept(context.Background(), event); err != nil {
			t.Fatalf("Accept(%s): %v", event.ID, err)
		}
		if idx < len(acceptedEvents)-1 {
			time.Sleep(2 * time.Millisecond)
		}
	}

	acceptedRecords, err := events.ListAccepted(context.Background())
	if err != nil {
		t.Fatalf("ListAccepted store snapshot: %v", err)
	}
	if len(acceptedRecords) != 3 {
		t.Fatalf("expected three accepted records, got %d", len(acceptedRecords))
	}

	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		From:  acceptedRecords[1].AcceptedAt,
		To:    acceptedRecords[2].AcceptedAt,
		Order: eventing.EventListOrderAsc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListAccepted time range: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("expected two accepted records in range, got %d", page.Total)
	}
	if len(page.Items) != 2 || page.Items[0].Event.ID != "accepted-time-2" || page.Items[1].Event.ID != "accepted-time-3" {
		t.Fatalf("unexpected accepted time-range page: %#v", page.Items)
	}
}

func TestEventPlaneControlListAccepted_CursorPagination(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	for idx, event := range []domain.Event{
		{ID: "accepted-cursor-1", Kind: domain.EventKindAgentOnline, DeviceID: "dev-cursor"},
		{ID: "accepted-cursor-2", Kind: domain.EventKindScreenChanged, DeviceID: "dev-cursor", SeqNo: 1},
		{ID: "accepted-cursor-3", Kind: domain.EventKindAccessibilityDisabled, DeviceID: "dev-cursor", SeqNo: 2},
	} {
		if _, err := events.Accept(context.Background(), event); err != nil {
			t.Fatalf("Accept(%s): %v", event.ID, err)
		}
		if idx < 2 {
			time.Sleep(2 * time.Millisecond)
		}
	}

	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page1, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		Order: eventing.EventListOrderDesc,
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListAccepted page1: %v", err)
	}
	if !page1.HasMore || page1.NextCursor == "" {
		t.Fatalf("expected next cursor on first page, got %+v", page1)
	}
	if len(page1.Items) != 2 || page1.Items[0].Event.ID != "accepted-cursor-3" || page1.Items[1].Event.ID != "accepted-cursor-2" {
		t.Fatalf("unexpected first cursor page: %#v", page1.Items)
	}

	page2, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		Order:  eventing.EventListOrderDesc,
		Limit:  2,
		Cursor: page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("ListAccepted page2: %v", err)
	}
	if page2.HasMore || page2.NextCursor != "" {
		t.Fatalf("expected terminal cursor page, got %+v", page2)
	}
	if len(page2.Items) != 1 || page2.Items[0].Event.ID != "accepted-cursor-1" {
		t.Fatalf("unexpected second cursor page: %#v", page2.Items)
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

	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page, err := uc.ListDeadLetters(context.Background(), eventing.DeadLetterListQuery{
		EventID: "event-a",
		Source:  "orchestrator",
		Order:   eventing.EventListOrderAsc,
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

func TestEventPlaneControlListDeadLetters_FiltersByRecordedTimeRange(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	base := time.Now().UTC().Add(-10 * time.Minute)
	records := []domain.DeadLetterRecord{
		domain.NewDeadLetterRecord(&domain.Event{ID: "event-time-a", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-time"}, nil, "first", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "event-time-b", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-time"}, nil, "second", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "event-time-c", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-time"}, nil, "third", "orchestrator"),
	}
	records[0].RecordedAt = base
	records[1].RecordedAt = base.Add(1 * time.Minute)
	records[2].RecordedAt = base.Add(2 * time.Minute)
	for _, record := range records {
		if err := events.RecordDeadLetter(context.Background(), record); err != nil {
			t.Fatalf("RecordDeadLetter(%s): %v", record.ID, err)
		}
	}

	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page, err := uc.ListDeadLetters(context.Background(), eventing.DeadLetterListQuery{
		From:  base.Add(30 * time.Second),
		To:    base.Add(90 * time.Second),
		Order: eventing.EventListOrderAsc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListDeadLetters time range: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Reason != "second" {
		t.Fatalf("unexpected dead-letter time-range page: %#v", page.Items)
	}
}

func TestEventPlaneControlListDeadLetters_CursorPagination(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	base := time.Now().UTC().Add(-20 * time.Minute)
	records := []domain.DeadLetterRecord{
		domain.NewDeadLetterRecord(&domain.Event{ID: "dead-cursor-a", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-cursor"}, nil, "first", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "dead-cursor-b", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-cursor"}, nil, "second", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "dead-cursor-c", Kind: domain.EventKindToolResult, DeviceID: "dev-dead-cursor"}, nil, "third", "orchestrator"),
	}
	for idx := range records {
		records[idx].RecordedAt = base.Add(time.Duration(idx) * time.Minute)
		if err := events.RecordDeadLetter(context.Background(), records[idx]); err != nil {
			t.Fatalf("RecordDeadLetter(%s): %v", records[idx].ID, err)
		}
	}

	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())
	page1, err := uc.ListDeadLetters(context.Background(), eventing.DeadLetterListQuery{
		Order: eventing.EventListOrderAsc,
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListDeadLetters page1: %v", err)
	}
	if !page1.HasMore || page1.NextCursor == "" {
		t.Fatalf("expected next cursor on first dead-letter page, got %+v", page1)
	}
	if len(page1.Items) != 2 || page1.Items[0].Reason != "first" || page1.Items[1].Reason != "second" {
		t.Fatalf("unexpected first dead-letter cursor page: %#v", page1.Items)
	}

	page2, err := uc.ListDeadLetters(context.Background(), eventing.DeadLetterListQuery{
		Order:  eventing.EventListOrderAsc,
		Limit:  2,
		Cursor: page1.NextCursor,
	})
	if err != nil {
		t.Fatalf("ListDeadLetters page2: %v", err)
	}
	if page2.HasMore || page2.NextCursor != "" {
		t.Fatalf("expected terminal dead-letter cursor page, got %+v", page2)
	}
	if len(page2.Items) != 1 || page2.Items[0].Reason != "third" {
		t.Fatalf("unexpected second dead-letter cursor page: %#v", page2.Items)
	}
}

func TestEventPlaneControlListAccepted_InvalidQuery(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())

	_, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		Limit: eventing.MaxEventListLimit + 1,
	})
	if err == nil {
		t.Fatal("expected invalid query error")
	}
	if !errors.Is(err, eventing.ErrInvalidEventListQuery) {
		t.Fatalf("expected ErrInvalidEventListQuery, got %v", err)
	}
}

func TestEventPlaneControlListAccepted_InvalidTimeRange(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())

	_, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		From: time.Now().UTC(),
		To:   time.Now().UTC().Add(-1 * time.Minute),
	})
	if err == nil {
		t.Fatal("expected invalid time range error")
	}
	if !errors.Is(err, eventing.ErrInvalidEventListQuery) {
		t.Fatalf("expected ErrInvalidEventListQuery, got %v", err)
	}
}

func TestEventPlaneControlListAccepted_InvalidCursor(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())

	_, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		Cursor: "not-base64",
	})
	if err == nil {
		t.Fatal("expected invalid cursor error")
	}
	if !errors.Is(err, eventing.ErrInvalidEventListQuery) {
		t.Fatalf("expected ErrInvalidEventListQuery, got %v", err)
	}
}

func TestEventPlaneControlListAccepted_CursorAndOffsetInvalid(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	uc := eventing.NewEventPlaneControl(events, &replayAcceptedRecorder{}, &notificationReplayRecorder{}, newLog())

	_, err := uc.ListAccepted(context.Background(), eventing.AcceptedEventListQuery{
		Cursor: "eyJ2IjoxLCJvcmRlciI6ImRlc2MiLCJ0aW1lc3RhbXAiOiIyMDI2LTAzLTE0VDAwOjAwOjAwWiIsImlkIjoiZXZlbnQifQ",
		Offset: 1,
	})
	if err == nil {
		t.Fatal("expected cursor+offset invalid error")
	}
	if !errors.Is(err, eventing.ErrInvalidEventListQuery) {
		t.Fatalf("expected ErrInvalidEventListQuery, got %v", err)
	}
}

func TestEventPlaneControlReplayAccepted_FailureMetrics(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	event := domain.Event{
		ID:         "dev-accepted-fail:8",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   "dev-accepted-fail",
		OccurredAt: time.Now().UTC(),
	}
	if _, err := events.Accept(context.Background(), event); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	replayer := &replayAcceptedRecorder{err: errors.New("boom")}
	metrics := telemetry.NewRegistry()
	uc := eventing.NewEventPlaneControl(events, replayer, &notificationReplayRecorder{}, newLog(), metrics)

	if err := uc.ReplayAccepted(context.Background(), event.ID); err == nil {
		t.Fatal("expected replay accepted to fail")
	}
	if got := metrics.Snapshot().Replay[telemetry.ReplayPathAccepted][telemetry.ReplayOutcomeFailed]; got != 1 {
		t.Fatalf("expected accepted replay failure metric 1, got %d", got)
	}
}
