package projection_test

import (
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/projection"
)

func newTestLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// fakeHistory is an in-memory HistoryStore for hub tests.
type fakeHistory struct {
	events []projection.Event
	nextID uint64
}

func (f *fakeHistory) Append(event projection.Event) (projection.Event, error) {
	f.nextID++
	event.ID = fmt.Sprintf("%d", f.nextID)
	f.events = append(f.events, event)
	return event, nil
}

func (f *fakeHistory) ListAfter(afterID string, topics []string) ([]projection.Event, error) {
	var after uint64
	if afterID != "" {
		if _, err := fmt.Sscanf(afterID, "%d", &after); err != nil {
			return nil, projection.ErrInvalidCursor
		}
	}
	topicSet := make(map[string]struct{}, len(topics))
	for _, t := range topics {
		topicSet[t] = struct{}{}
	}
	var out []projection.Event
	for _, e := range f.events {
		var id uint64
		fmt.Sscanf(e.ID, "%d", &id) //nolint:errcheck
		if id <= after {
			continue
		}
		if len(topicSet) > 0 {
			if _, ok := topicSet[e.Topic]; !ok {
				continue
			}
		}
		out = append(out, e)
	}
	return out, nil
}

func TestHub_SubscribeReceivesPublishedEvent(t *testing.T) {
	hub := projection.NewHub(nil, newTestLog())

	ch, unsubscribe, err := hub.Subscribe(nil, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	hub.PublishProjection(projection.Event{Topic: "tasks", Type: "upsert", EntityID: "t-1"})

	select {
	case got := <-ch:
		if got.Topic != "tasks" || got.EntityID != "t-1" {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}
}

func TestHub_TopicFilter_ExcludesNonMatchingTopics(t *testing.T) {
	hub := projection.NewHub(nil, newTestLog())

	ch, unsubscribe, err := hub.Subscribe([]string{"tasks"}, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	hub.PublishProjection(projection.Event{Topic: "other", Type: "upsert", EntityID: "o-1"})
	hub.PublishProjection(projection.Event{Topic: "tasks", Type: "upsert", EntityID: "t-1"})

	select {
	case got := <-ch:
		if got.EntityID != "t-1" {
			t.Fatalf("expected t-1, got %+v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for event")
	}

	// No second event expected (other topic was filtered out).
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra event: %+v", extra)
	default:
	}
}

func TestHub_SubscribeWithAfterID_BackfillsMissedEvents(t *testing.T) {
	hist := &fakeHistory{}
	hub := projection.NewHub(hist, newTestLog())

	// Publish two events before subscribing.
	hub.PublishProjection(projection.Event{Topic: "tasks", Type: "upsert", EntityID: "t-1"})
	hub.PublishProjection(projection.Event{Topic: "tasks", Type: "upsert", EntityID: "t-2"})

	// Subscribe from after the first event — should backfill only t-2.
	ch, unsubscribe, err := hub.Subscribe([]string{"tasks"}, "1")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	select {
	case got := <-ch:
		if got.EntityID != "t-2" {
			t.Fatalf("expected t-2 as backfill, got %+v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for backfilled event")
	}
}

func TestHub_Unsubscribe_ClosesChannel(t *testing.T) {
	hub := projection.NewHub(nil, newTestLog())

	ch, unsubscribe, err := hub.Subscribe(nil, "")
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	unsubscribe()

	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected channel to be closed after unsubscribe")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("channel not closed after unsubscribe")
	}
}
