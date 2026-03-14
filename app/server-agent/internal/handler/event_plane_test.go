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
	event := domain.Event{
		ID:         "dev-http:1",
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   "dev-http",
		SeqNo:      1,
		OccurredAt: time.Now().UTC(),
	}
	if _, err := events.Accept(context.Background(), event); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/accepted", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/accepted: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload []domain.AcceptedEventRecord
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 1 || payload[0].Event.ID != event.ID {
		t.Fatalf("unexpected accepted payload: %#v", payload)
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
