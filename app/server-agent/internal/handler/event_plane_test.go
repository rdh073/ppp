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

func TestEventPlaneHandler_ListAccepted_ExcludePayload(t *testing.T) {
	h, events, _, _ := newEventPlaneHandler(t)
	event := domain.Event{
		ID:         "dev-http-payload:1",
		Kind:       domain.EventKindScreenChanged,
		DeviceID:   "dev-http-payload",
		SeqNo:      1,
		OccurredAt: time.Now().UTC(),
		Payload: map[string]any{
			"message": "large payload should be omitted",
		},
	}
	if _, err := events.Accept(context.Background(), event); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/accepted?deviceId=dev-http-payload&includePayload=false&limit=10", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/accepted includePayload=false: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload usecase.AcceptedEventPage
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(payload.Items))
	}
	if payload.Items[0].Event.Payload != nil {
		t.Fatalf("expected payload to be omitted, got %#v", payload.Items[0].Event.Payload)
	}
}

func TestEventPlaneHandler_ListAccepted_TimeRange(t *testing.T) {
	h, events, _, _ := newEventPlaneHandler(t)
	for idx, event := range []domain.Event{
		{ID: "dev-http-time:1", Kind: domain.EventKindAgentOnline, DeviceID: "dev-http-time"},
		{ID: "dev-http-time:2", Kind: domain.EventKindScreenChanged, DeviceID: "dev-http-time", SeqNo: 1},
		{ID: "dev-http-time:3", Kind: domain.EventKindAccessibilityDisabled, DeviceID: "dev-http-time", SeqNo: 2},
	} {
		if _, err := events.Accept(context.Background(), event); err != nil {
			t.Fatalf("Accept: %v", err)
		}
		if idx < 2 {
			time.Sleep(2 * time.Millisecond)
		}
	}
	records, err := events.ListAccepted(context.Background())
	if err != nil {
		t.Fatalf("ListAccepted snapshot: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 accepted records, got %d", len(records))
	}

	from := records[1].AcceptedAt.UTC().Format(time.RFC3339Nano)
	to := records[2].AcceptedAt.UTC().Format(time.RFC3339Nano)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/accepted?from="+from+"&to="+to+"&order=asc&limit=10", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/accepted time range: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload usecase.AcceptedEventPage
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 2 || len(payload.Items) != 2 {
		t.Fatalf("unexpected accepted time-range payload: %#v", payload)
	}
	if payload.Items[0].Event.ID != "dev-http-time:2" || payload.Items[1].Event.ID != "dev-http-time:3" {
		t.Fatalf("unexpected accepted time-range items: %#v", payload.Items)
	}
}

func TestEventPlaneHandler_ListAccepted_CursorPagination(t *testing.T) {
	h, events, _, _ := newEventPlaneHandler(t)
	for idx, event := range []domain.Event{
		{ID: "dev-http-cursor:1", Kind: domain.EventKindAgentOnline, DeviceID: "dev-http-cursor"},
		{ID: "dev-http-cursor:2", Kind: domain.EventKindScreenChanged, DeviceID: "dev-http-cursor", SeqNo: 1},
		{ID: "dev-http-cursor:3", Kind: domain.EventKindAccessibilityDisabled, DeviceID: "dev-http-cursor", SeqNo: 2},
	} {
		if _, err := events.Accept(context.Background(), event); err != nil {
			t.Fatalf("Accept: %v", err)
		}
		if idx < 2 {
			time.Sleep(2 * time.Millisecond)
		}
	}

	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/events/accepted?order=desc&limit=2", nil)
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("GET /events/accepted page1: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}
	var page1 usecase.AcceptedEventPage
	if err := json.NewDecoder(rec1.Body).Decode(&page1); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if !page1.HasMore || page1.NextCursor == "" {
		t.Fatalf("expected next cursor, got %#v", page1)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/events/accepted?order=desc&limit=2&cursor="+page1.NextCursor, nil)
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /events/accepted page2: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var page2 usecase.AcceptedEventPage
	if err := json.NewDecoder(rec2.Body).Decode(&page2); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if page2.HasMore || page2.NextCursor != "" {
		t.Fatalf("expected terminal second page, got %#v", page2)
	}
	if len(page2.Items) != 1 || page2.Items[0].Event.ID != "dev-http-cursor:1" {
		t.Fatalf("unexpected second cursor page: %#v", page2.Items)
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

func TestEventPlaneHandler_ListAccepted_InvalidFrom_400(t *testing.T) {
	h, _, _, _ := newEventPlaneHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/accepted?from=not-a-timestamp", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /events/accepted with invalid from: expected 400, got %d", rec.Code)
	}
}

func TestEventPlaneHandler_ListAccepted_InvalidCursor_400(t *testing.T) {
	h, _, _, _ := newEventPlaneHandler(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/accepted?cursor=not-base64", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /events/accepted with invalid cursor: expected 400, got %d", rec.Code)
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

func TestEventPlaneHandler_ListDeadLetters_TimeRange(t *testing.T) {
	h, events, _, _ := newEventPlaneHandler(t)
	base := time.Now().UTC().Add(-5 * time.Minute)
	records := []domain.DeadLetterRecord{
		domain.NewDeadLetterRecord(&domain.Event{ID: "event-http-a", Kind: domain.EventKindToolResult, DeviceID: "dev-http-dead-time"}, nil, "first", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "event-http-b", Kind: domain.EventKindToolResult, DeviceID: "dev-http-dead-time"}, nil, "second", "orchestrator"),
		domain.NewDeadLetterRecord(&domain.Event{ID: "event-http-c", Kind: domain.EventKindToolResult, DeviceID: "dev-http-dead-time"}, nil, "third", "orchestrator"),
	}
	records[0].RecordedAt = base
	records[1].RecordedAt = base.Add(1 * time.Minute)
	records[2].RecordedAt = base.Add(2 * time.Minute)
	for _, record := range records {
		if err := events.RecordDeadLetter(context.Background(), record); err != nil {
			t.Fatalf("RecordDeadLetter: %v", err)
		}
	}

	from := base.Add(30 * time.Second).Format(time.RFC3339Nano)
	to := base.Add(90 * time.Second).Format(time.RFC3339Nano)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/deadletters?from="+from+"&to="+to+"&order=asc&limit=10", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /events/deadletters time range: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var payload usecase.DeadLetterPage
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 1 || len(payload.Items) != 1 || payload.Items[0].Reason != "second" {
		t.Fatalf("unexpected dead-letter time-range payload: %#v", payload)
	}
}
