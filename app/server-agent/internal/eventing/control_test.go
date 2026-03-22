package eventing

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

func TestEventPlaneControl_GetAcceptedPayloadTruncates(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	_, err := events.Accept(context.Background(), domain.Event{
		ID:         "dev-1:7",
		Kind:       domain.EventKindNotification,
		DeviceID:   "dev-1",
		SeqNo:      7,
		OccurredAt: time.Now(),
		Payload: map[string]any{
			"message": strings.Repeat("x", 64),
		},
	})
	if err != nil {
		t.Fatalf("Accept() error = %v", err)
	}

	control := NewEventPlaneControl(events, &fakeAcceptedReplayer{}, &fakeNotificationReplayer{}, testLogger())
	preview, err := control.GetAcceptedPayload(context.Background(), "dev-1:7", 16)
	if err != nil {
		t.Fatalf("GetAcceptedPayload() error = %v", err)
	}
	if !preview.Truncated {
		t.Fatal("expected payload preview to be truncated")
	}
	if preview.SizeBytes <= len(preview.PayloadText) {
		t.Fatalf("expected preview sizeBytes > payloadText len, got %d <= %d", preview.SizeBytes, len(preview.PayloadText))
	}
}

func TestEventPlaneControl_ReplayDeadLetter_UsesNotificationIngestForIngestionSource(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	raw := json.RawMessage(`{"seqNo":42,"reason":"disabled"}`)
	if err := events.RecordDeadLetter(context.Background(), domain.DeadLetterRecord{
		ID:         "dead-ingest-1",
		Kind:       domain.EventKindAccessibilityDisabled,
		DeviceID:   "dev-1",
		SeqNo:      42,
		Payload:    raw,
		Source:     "ingestion",
		RecordedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordDeadLetter() error = %v", err)
	}

	accepted := &fakeAcceptedReplayer{}
	notifications := &fakeNotificationReplayer{}
	control := NewEventPlaneControl(events, accepted, notifications, testLogger())

	if err := control.ReplayDeadLetter(context.Background(), "dead-ingest-1"); err != nil {
		t.Fatalf("ReplayDeadLetter() error = %v", err)
	}
	if len(notifications.calls) != 1 {
		t.Fatalf("notification replay calls = %d, want 1", len(notifications.calls))
	}
	if len(accepted.events) != 0 {
		t.Fatalf("accepted replay events = %d, want 0", len(accepted.events))
	}
	if notifications.calls[0].method != string(domain.EventKindAccessibilityDisabled) {
		t.Fatalf("notification method = %q", notifications.calls[0].method)
	}
}

func TestEventPlaneControl_ReplayDeadLetter_UsesAcceptedReplayForNonIngestionSource(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	raw := json.RawMessage(`{"hello":"world"}`)
	if err := events.RecordDeadLetter(context.Background(), domain.DeadLetterRecord{
		ID:         "dead-accepted-1",
		Kind:       domain.EventKindNotification,
		DeviceID:   "dev-2",
		SeqNo:      9,
		Payload:    raw,
		Source:     "accepted",
		RecordedAt: time.Now(),
	}); err != nil {
		t.Fatalf("RecordDeadLetter() error = %v", err)
	}

	accepted := &fakeAcceptedReplayer{}
	notifications := &fakeNotificationReplayer{}
	control := NewEventPlaneControl(events, accepted, notifications, testLogger())

	if err := control.ReplayDeadLetter(context.Background(), "dead-accepted-1"); err != nil {
		t.Fatalf("ReplayDeadLetter() error = %v", err)
	}
	if len(accepted.events) != 1 {
		t.Fatalf("accepted replay events = %d, want 1", len(accepted.events))
	}
	if len(notifications.calls) != 0 {
		t.Fatalf("notification replay calls = %d, want 0", len(notifications.calls))
	}
	if got, want := accepted.events[0].ID, "dev-2:9"; got != want {
		t.Fatalf("replayed event id = %q, want %q", got, want)
	}
}

type fakeAcceptedReplayer struct {
	events []domain.Event
}

func (f *fakeAcceptedReplayer) ReplayAcceptedEvent(_ context.Context, event domain.Event) error {
	f.events = append(f.events, event)
	return nil
}

type notificationCall struct {
	deviceID domain.DeviceID
	method   string
	raw      json.RawMessage
}

type fakeNotificationReplayer struct {
	calls []notificationCall
}

func (f *fakeNotificationReplayer) IngestNotification(_ context.Context, deviceID domain.DeviceID, method string, rawParams json.RawMessage) error {
	f.calls = append(f.calls, notificationCall{
		deviceID: deviceID,
		method:   method,
		raw:      append(json.RawMessage(nil), rawParams...),
	})
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
