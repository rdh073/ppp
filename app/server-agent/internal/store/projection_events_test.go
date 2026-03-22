package store_test

import (
	"errors"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/projection"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

func TestFileProjectionEventStore_PersistsAndReplaysByCursor(t *testing.T) {
	dir := t.TempDir()
	s, err := store.NewFileProjectionEventStore(dir, 8)
	if err != nil {
		t.Fatalf("NewFileProjectionEventStore: %v", err)
	}

	first, err := s.Append(projection.Event{Topic: "account-manager.accounts", Type: "upsert", EntityID: "acc-1", Payload: map[string]string{"id": "acc-1"}})
	if err != nil {
		t.Fatalf("Append(first): %v", err)
	}
	second, err := s.Append(projection.Event{Topic: "account-manager.accounts", Type: "upsert", EntityID: "acc-2", Payload: map[string]string{"id": "acc-2"}})
	if err != nil {
		t.Fatalf("Append(second): %v", err)
	}

	reopened, err := store.NewFileProjectionEventStore(dir, 8)
	if err != nil {
		t.Fatalf("NewFileProjectionEventStore(reopen): %v", err)
	}
	items, err := reopened.ListAfter(first.ID, []string{"account-manager.accounts"})
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 replay item, got %d", len(items))
	}
	if items[0].ID != second.ID || items[0].EntityID != "acc-2" {
		t.Fatalf("unexpected replay item: %#v", items[0])
	}
}

func TestMemoryProjectionEventStore_ReturnsBackfillUnavailableWhenCursorIsPruned(t *testing.T) {
	s := store.NewMemoryProjectionEventStore(1)
	for _, entityID := range []string{"acc-1", "acc-2", "acc-3"} {
		if _, err := s.Append(projection.Event{Topic: "account-manager.accounts", Type: "upsert", EntityID: entityID}); err != nil {
			t.Fatalf("Append(%s): %v", entityID, err)
		}
	}

	_, err := s.ListAfter("1", []string{"account-manager.accounts"})
	if !errors.Is(err, projection.ErrBackfillUnavailable) {
		t.Fatalf("expected ErrBackfillUnavailable, got %v", err)
	}
}

func TestMemoryProjectionEventStore_IDsAreMonotonicallyIncreasing(t *testing.T) {
	s := store.NewMemoryProjectionEventStore(8)

	first, err := s.Append(projection.Event{Topic: "tasks", Type: "upsert", EntityID: "t-1"})
	if err != nil {
		t.Fatalf("Append(first): %v", err)
	}
	second, err := s.Append(projection.Event{Topic: "tasks", Type: "upsert", EntityID: "t-2"})
	if err != nil {
		t.Fatalf("Append(second): %v", err)
	}

	if first.ID == "" {
		t.Fatal("expected non-empty ID for first event")
	}
	if second.ID == "" {
		t.Fatal("expected non-empty ID for second event")
	}
	if first.ID >= second.ID {
		// IDs are numeric strings; lexicographic compare is safe for same-length strings.
		// Compare numerically via ListAfter to avoid string-ordering ambiguity.
		items, err := s.ListAfter(first.ID, nil)
		if err != nil {
			t.Fatalf("ListAfter(first.ID): %v", err)
		}
		if len(items) == 0 || items[0].ID != second.ID {
			t.Fatalf("second event not found after first cursor: first=%s second=%s", first.ID, second.ID)
		}
	}
}

func TestMemoryProjectionEventStore_ListAfterEmptyCursor_ReturnsAllEvents(t *testing.T) {
	s := store.NewMemoryProjectionEventStore(8)

	for _, entityID := range []string{"t-1", "t-2", "t-3"} {
		if _, err := s.Append(projection.Event{Topic: "tasks", Type: "upsert", EntityID: entityID}); err != nil {
			t.Fatalf("Append(%s): %v", entityID, err)
		}
	}

	items, err := s.ListAfter("", nil)
	if err != nil {
		t.Fatalf("ListAfter(empty): %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 events from empty cursor, got %d", len(items))
	}
}
