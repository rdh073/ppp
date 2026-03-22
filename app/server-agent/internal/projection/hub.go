package projection

import (
	"log/slog"
	"sync"
	"time"
)

type Hub struct {
	mu      sync.Mutex
	nextID  int
	subs    map[int]subscriber
	history HistoryStore
	log     *slog.Logger
}

type subscriber struct {
	topics map[string]struct{}
	ch     chan Event
}

func NewHub(history HistoryStore, log *slog.Logger) *Hub {
	return &Hub{
		subs:    make(map[int]subscriber),
		history: history,
		log:     log,
	}
}

func (h *Hub) PublishProjection(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	event = h.storeLocked(event)
	for _, sub := range h.subs {
		if len(sub.topics) > 0 {
			if _, ok := sub.topics[event.Topic]; !ok {
				continue
			}
		}
		select {
		case sub.ch <- event:
		default:
			// Slow subscribers are lossy by design; reconnects use Last-Event-ID
			// against the projection history store to backfill missed updates.
		}
	}
}

func (h *Hub) Subscribe(topics []string, afterID string) (<-chan Event, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	var backlog []Event
	var err error
	if afterID != "" && h.history != nil {
		backlog, err = h.history.ListAfter(afterID, topics)
		if err != nil {
			return nil, nil, err
		}
	}

	h.nextID++
	id := h.nextID
	ch := make(chan Event, maxInt(32, len(backlog)+8))
	for _, event := range backlog {
		ch <- event
	}
	h.subs[id] = subscriber{
		topics: makeTopicSet(topics),
		ch:     ch,
	}

	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		sub, ok := h.subs[id]
		if !ok {
			return
		}
		delete(h.subs, id)
		close(sub.ch)
	}, nil
}

func (h *Hub) storeLocked(event Event) Event {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if h.history == nil {
		return event
	}
	stored, err := h.history.Append(event)
	if err != nil {
		if h.log != nil {
			h.log.Warn("persist projection event", "topic", event.Topic, "entityId", event.EntityID, "err", err)
		}
		return event
	}
	return stored
}

func makeTopicSet(topics []string) map[string]struct{} {
	if len(topics) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		if topic == "" {
			continue
		}
		out[topic] = struct{}{}
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
