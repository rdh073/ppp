package projection

import (
	"errors"
	"time"
)

var (
	ErrInvalidCursor       = errors.New("invalid projection cursor")
	ErrBackfillUnavailable = errors.New("projection backfill unavailable")
)

type Event struct {
	ID         string    `json:"id,omitempty"`
	Topic      string    `json:"topic"`
	Type       string    `json:"type"`
	EntityID   string    `json:"entityId"`
	OccurredAt time.Time `json:"occurredAt"`
	Payload    any       `json:"payload"`
}

type Publisher interface {
	PublishProjection(event Event)
}

type HistoryStore interface {
	Append(event Event) (Event, error)
	ListAfter(afterID string, topics []string) ([]Event, error)
}
