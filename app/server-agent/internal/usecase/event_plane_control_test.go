package usecase_test

import (
	"context"
	"encoding/json"
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
