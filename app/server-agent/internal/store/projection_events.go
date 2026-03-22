package store

import (
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/projection"
)

const (
	projectionEventsSnapshotFilename = "projection_events.json"
	defaultProjectionRetention       = 2048
)

type projectionEventSnapshot struct {
	NextID uint64             `json:"nextId"`
	Events []projection.Event `json:"events"`
}

type MemoryProjectionEventStore struct {
	mu        sync.Mutex
	retention int
	snapshot  projectionEventSnapshot
}

func NewMemoryProjectionEventStore(retention int) *MemoryProjectionEventStore {
	if retention <= 0 {
		retention = defaultProjectionRetention
	}
	return &MemoryProjectionEventStore{retention: retention}
}

func (s *MemoryProjectionEventStore) Append(event projection.Event) (projection.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := nextProjectionEvent(&s.snapshot, event, s.retention)
	return stored, nil
}

func (s *MemoryProjectionEventStore) ListAfter(afterID string, topics []string) ([]projection.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return projectionEventsAfter(s.snapshot, afterID, topics)
}

type FileProjectionEventStore struct {
	mu        sync.Mutex
	path      string
	retention int
	snapshot  projectionEventSnapshot
}

func NewFileProjectionEventStore(dir string, retention int) (*FileProjectionEventStore, error) {
	if retention <= 0 {
		retention = defaultProjectionRetention
	}
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	store := &FileProjectionEventStore{
		path:      filepath.Join(dir, projectionEventsSnapshotFilename),
		retention: retention,
		snapshot: projectionEventSnapshot{
			Events: make([]projection.Event, 0),
		},
	}
	if err := loadJSONFile(store.path, &store.snapshot); err != nil {
		return nil, fmt.Errorf("load projection event snapshot: %w", err)
	}
	return store, nil
}

func (s *FileProjectionEventStore) Append(event projection.Event) (projection.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := nextProjectionEvent(&s.snapshot, event, s.retention)
	if err := writeJSONFileAtomically(s.path, s.snapshot); err != nil {
		return projection.Event{}, err
	}
	return stored, nil
}

func (s *FileProjectionEventStore) ListAfter(afterID string, topics []string) ([]projection.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return projectionEventsAfter(s.snapshot, afterID, topics)
}

func nextProjectionEvent(snapshot *projectionEventSnapshot, event projection.Event, retention int) projection.Event {
	snapshot.NextID++
	stored := cloneProjectionEvent(event)
	stored.ID = strconv.FormatUint(snapshot.NextID, 10)
	if stored.OccurredAt.IsZero() {
		stored.OccurredAt = time.Now().UTC()
	}
	snapshot.Events = append(snapshot.Events, stored)
	if retention > 0 && len(snapshot.Events) > retention {
		snapshot.Events = append([]projection.Event(nil), snapshot.Events[len(snapshot.Events)-retention:]...)
	}
	return cloneProjectionEvent(stored)
}

func projectionEventsAfter(snapshot projectionEventSnapshot, afterID string, topics []string) ([]projection.Event, error) {
	after, err := parseProjectionID(afterID)
	if err != nil {
		return nil, err
	}
	if after > 0 && len(snapshot.Events) > 0 {
		oldest, err := parseProjectionID(snapshot.Events[0].ID)
		if err != nil {
			return nil, err
		}
		if oldest > after+1 {
			return nil, projection.ErrBackfillUnavailable
		}
	}
	topicSet := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		if topic == "" {
			continue
		}
		topicSet[topic] = struct{}{}
	}
	out := make([]projection.Event, 0)
	for _, event := range snapshot.Events {
		id, err := parseProjectionID(event.ID)
		if err != nil {
			return nil, err
		}
		if id <= after {
			continue
		}
		if len(topicSet) > 0 {
			if _, ok := topicSet[event.Topic]; !ok {
				continue
			}
		}
		out = append(out, cloneProjectionEvent(event))
	}
	return out, nil
}

func parseProjectionID(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", projection.ErrInvalidCursor, raw)
	}
	return id, nil
}

func cloneProjectionEvent(event projection.Event) projection.Event {
	cp := event
	return cp
}
