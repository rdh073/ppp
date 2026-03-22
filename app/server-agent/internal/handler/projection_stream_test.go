package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/projection"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

func TestProjectionStreamHandler_BackfillsMissedEventsFromLastEventID(t *testing.T) {
	history := store.NewMemoryProjectionEventStore(8)
	hub := projection.NewHub(history, newLog())
	stream := handler.NewProjectionStreamHandler(hub)

	hub.PublishProjection(projection.Event{Topic: "account-manager.accounts", Type: "upsert", EntityID: "acc-1", Payload: map[string]string{"id": "acc-1"}})
	hub.PublishProjection(projection.Event{Topic: "account-manager.accounts", Type: "upsert", EntityID: "acc-2", Payload: map[string]string{"id": "acc-2"}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/events/stream?topics=account-manager.accounts", nil).WithContext(ctx)
	req.Header.Set("Last-Event-ID", "1")
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		stream.ServeHTTP(rec, req)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()
	if !strings.Contains(body, "id: 2\n") {
		t.Fatalf("expected replayed event id in body, got %q", body)
	}
	if !strings.Contains(body, `"entityId":"acc-2"`) {
		t.Fatalf("expected replayed event payload in body, got %q", body)
	}
}

func TestProjectionStreamHandler_EmitsResetWhenBackfillUnavailable(t *testing.T) {
	history := store.NewMemoryProjectionEventStore(1)
	hub := projection.NewHub(history, newLog())
	stream := handler.NewProjectionStreamHandler(hub)

	for _, entityID := range []string{"acc-1", "acc-2", "acc-3"} {
		hub.PublishProjection(projection.Event{Topic: "account-manager.accounts", Type: "upsert", EntityID: entityID})
	}

	req := httptest.NewRequest(http.MethodGet, "/events/stream?topics=account-manager.accounts", nil)
	req.Header.Set("Last-Event-ID", "1")
	rec := httptest.NewRecorder()
	stream.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: reset\n") {
		t.Fatalf("expected reset event, got %q", body)
	}
	if !strings.Contains(body, "projection history unavailable") {
		t.Fatalf("expected reset reason, got %q", body)
	}
}
