package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

type acceptedReplayStub struct {
	events []domain.Event
}

func (s *acceptedReplayStub) ReplayAcceptedEvent(_ context.Context, event domain.Event) error {
	s.events = append(s.events, event)
	return nil
}

type notificationReplayStub struct {
	deviceID domain.DeviceID
	method   string
}

func (s *notificationReplayStub) IngestNotification(
	_ context.Context,
	deviceID domain.DeviceID,
	method string,
	_ json.RawMessage,
) error {
	s.deviceID = deviceID
	s.method = method
	return nil
}

func newEventPlaneHandler(t *testing.T) (http.Handler, *store.MemoryEventPlaneStore, *acceptedReplayStub, *notificationReplayStub) {
	t.Helper()
	events := store.NewMemoryEventPlaneStore()
	accepted := &acceptedReplayStub{}
	notifications := &notificationReplayStub{}
	uc := usecase.NewEventPlaneControl(events, accepted, notifications, newLog())
	return handler.NewEventPlaneHandler(uc, newLog()), events, accepted, notifications
}

func newLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestEventPlaneHandler_ListAccepted(t *testing.T) {
	h, events, _, _ := newEventPlaneHandler(t)
	now := time.Now().UTC()
	for _, event := range []domain.Event{
		{
			ID:         "dev-http:1",
			Kind:       domain.EventKindScreenChanged,
			DeviceID:   "dev-http",
			SeqNo:      1,
			OccurredAt: now.Add(-2 * time.Minute),
		},
		{
			ID:         "dev-http:2",
			Kind:       domain.EventKindAccessibilityDisabled,
			DeviceID:   "dev-http",
			SeqNo:      2,
			OccurredAt: now.Add(-1 * time.Minute),
		},
		{
			ID:         "dev-http-other",
			Kind:       domain.EventKindAgentOnline,
			DeviceID:   "other-device",
			OccurredAt: now,
		},
	} {
		if _, err := events.Accept(context.Background(), event); err != nil {
			t.Fatalf("Accept: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/accepted?deviceId=dev-http&source=device&limit=1", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/accepted: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload usecase.AcceptedEventPage
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 2 || payload.Limit != 1 || payload.Offset != 0 || !payload.HasMore {
		t.Fatalf("unexpected pagination metadata: %#v", payload)
	}
	if len(payload.Items) != 1 || payload.Items[0].Event.ID != "dev-http:2" {
		t.Fatalf("unexpected accepted payload: %#v", payload)
	}
}

func TestEventPlaneHandler_ListAccepted_InvalidLimit_400(t *testing.T) {
	h, _, _, _ := newEventPlaneHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/accepted?limit=bad", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /events/accepted with invalid limit: expected 400, got %d", rec.Code)
	}
}

func TestEventPlaneHandler_ReplayDeadLetter(t *testing.T) {
	h, events, _, notifications := newEventPlaneHandler(t)
	record := domain.NewDeadLetterRecord(&domain.Event{
		Kind:     domain.EventKindAccessibilityDisabled,
		DeviceID: "dev-http-dead",
		Payload:  json.RawMessage(`{"seqNo":5}`),
	}, json.RawMessage(`{"seqNo":5}`), "decode params", "ingestion")
	if err := events.RecordDeadLetter(context.Background(), record); err != nil {
		t.Fatalf("RecordDeadLetter: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/events/deadletters/"+record.ID+"/replay", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /events/deadletters/{id}/replay: expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if notifications.deviceID != "dev-http-dead" || notifications.method != string(domain.EventKindAccessibilityDisabled) {
		t.Fatalf("unexpected replay target: device=%q method=%q", notifications.deviceID, notifications.method)
	}
}

func TestEventPlaneHandler_ListDeadLetters_FilterBySource(t *testing.T) {
	h, events, _, _ := newEventPlaneHandler(t)
	records := []domain.DeadLetterRecord{
		domain.NewDeadLetterRecord(&domain.Event{
			ID:       "event-a",
			Kind:     domain.EventKindToolResult,
			DeviceID: "dev-http-deadletters",
		}, nil, "first", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{
			ID:       "event-b",
			Kind:     domain.EventKindAccessibilityDisabled,
			DeviceID: "dev-http-deadletters",
		}, nil, "second", "ingestion"),
	}
	for _, record := range records {
		if err := events.RecordDeadLetter(context.Background(), record); err != nil {
			t.Fatalf("RecordDeadLetter: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/deadletters?source=orchestrator&limit=10&order=asc", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/deadletters: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload usecase.DeadLetterPage
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].Reason != "first" {
		t.Fatalf("unexpected dead-letter payload: %#v", payload)
	}
}
