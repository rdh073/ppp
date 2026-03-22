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
