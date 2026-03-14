package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

var ErrInvalidEventListQuery = errors.New("invalid event list query")

const (
	DefaultEventListLimit = 100
	MaxEventListLimit     = 500
)

type EventListOrder string

const (
	EventListOrderAsc  EventListOrder = "asc"
	EventListOrderDesc EventListOrder = "desc"
)

type AcceptedEventListQuery struct {
	DeviceID domain.DeviceID
	Kind     domain.EventKind
	Source   string
	From     time.Time
	To       time.Time
	Cursor   string
	Order    EventListOrder
	Limit    int
	Offset   int

	cursor *eventListCursor
}

type AcceptedEventPage struct {
	Items      []domain.AcceptedEventRecord `json:"items"`
	Total      int                          `json:"total"`
	Offset     int                          `json:"offset"`
	Limit      int                          `json:"limit"`
	HasMore    bool                         `json:"hasMore"`
	NextCursor string                       `json:"nextCursor,omitempty"`
}

type DeadLetterListQuery struct {
	DeviceID domain.DeviceID
	Kind     domain.EventKind
	Source   string
	EventID  string
	From     time.Time
	To       time.Time
	Cursor   string
	Order    EventListOrder
	Limit    int
	Offset   int

	cursor *eventListCursor
}

type DeadLetterPage struct {
	Items      []domain.DeadLetterRecord `json:"items"`
	Total      int                       `json:"total"`
	Offset     int                       `json:"offset"`
	Limit      int                       `json:"limit"`
	HasMore    bool                      `json:"hasMore"`
	NextCursor string                    `json:"nextCursor,omitempty"`
}

type eventListCursor struct {
	Version   int            `json:"v"`
	Order     EventListOrder `json:"order"`
	Timestamp time.Time      `json:"timestamp"`
	ID        string         `json:"id"`
}

func queryAcceptedRecords(records []domain.AcceptedEventRecord, query AcceptedEventListQuery) (AcceptedEventPage, error) {
	query, err := normalizeAcceptedEventListQuery(query)
	if err != nil {
		return AcceptedEventPage{}, err
	}

	items := make([]domain.AcceptedEventRecord, 0, query.Limit)
	total := 0
	skipped := 0
	started := query.cursor == nil
	hasMore := false

	iterateAcceptedRecords(records, query.Order, func(record domain.AcceptedEventRecord) bool {
		if !acceptedEventMatches(record, query) {
			return true
		}
		total++
		if !started {
			if isAcceptedEventAfterCursor(record, *query.cursor) {
				started = true
			} else {
				return true
			}
		}
		if skipped < query.Offset {
			skipped++
			return true
		}
		if len(items) < query.Limit {
			items = append(items, cloneAcceptedEventRecord(record))
			return true
		}
		hasMore = true
		return true
	})

	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = encodeEventListCursor(eventListCursor{
			Version:   1,
			Order:     query.Order,
			Timestamp: items[len(items)-1].AcceptedAt.UTC(),
			ID:        acceptedEventCursorID(items[len(items)-1]),
		})
	}

	return AcceptedEventPage{
		Items:      items,
		Total:      total,
		Offset:     query.Offset,
		Limit:      query.Limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func queryDeadLetterRecords(records []domain.DeadLetterRecord, query DeadLetterListQuery) (DeadLetterPage, error) {
	query, err := normalizeDeadLetterListQuery(query)
	if err != nil {
		return DeadLetterPage{}, err
	}

	items := make([]domain.DeadLetterRecord, 0, query.Limit)
	total := 0
	skipped := 0
	started := query.cursor == nil
	hasMore := false

	iterateDeadLetterRecords(records, query.Order, func(record domain.DeadLetterRecord) bool {
		if !deadLetterMatches(record, query) {
			return true
		}
		total++
		if !started {
			if isDeadLetterAfterCursor(record, *query.cursor) {
				started = true
			} else {
				return true
			}
		}
		if skipped < query.Offset {
			skipped++
			return true
		}
		if len(items) < query.Limit {
			items = append(items, cloneDeadLetterRecord(record))
			return true
		}
		hasMore = true
		return true
	})

	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = encodeEventListCursor(eventListCursor{
			Version:   1,
			Order:     query.Order,
			Timestamp: items[len(items)-1].RecordedAt.UTC(),
			ID:        items[len(items)-1].ID,
		})
	}

	return DeadLetterPage{
		Items:      items,
		Total:      total,
		Offset:     query.Offset,
		Limit:      query.Limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

func normalizeAcceptedEventListQuery(query AcceptedEventListQuery) (AcceptedEventListQuery, error) {
	rawOrder := query.Order
	limit, offset, order, err := normalizeEventListWindow(query.Limit, query.Offset, query.Order)
	if err != nil {
		return AcceptedEventListQuery{}, err
	}
	from, to, err := normalizeEventListTimeRange(query.From, query.To)
	if err != nil {
		return AcceptedEventListQuery{}, err
	}
	cursor, err := decodeEventListCursor(query.Cursor)
	if err != nil {
		return AcceptedEventListQuery{}, err
	}
	if cursor != nil {
		if offset != 0 {
			return AcceptedEventListQuery{}, fmt.Errorf("%w: cursor and offset cannot be combined", ErrInvalidEventListQuery)
		}
		if rawOrder == "" {
			order = cursor.Order
		} else if order != cursor.Order {
			return AcceptedEventListQuery{}, fmt.Errorf("%w: cursor order mismatch", ErrInvalidEventListQuery)
		}
	}
	query.Limit = limit
	query.Offset = offset
	query.Order = order
	query.From = from
	query.To = to
	query.cursor = cursor
	return query, nil
}

func normalizeDeadLetterListQuery(query DeadLetterListQuery) (DeadLetterListQuery, error) {
	rawOrder := query.Order
	limit, offset, order, err := normalizeEventListWindow(query.Limit, query.Offset, query.Order)
	if err != nil {
		return DeadLetterListQuery{}, err
	}
	from, to, err := normalizeEventListTimeRange(query.From, query.To)
	if err != nil {
		return DeadLetterListQuery{}, err
	}
	cursor, err := decodeEventListCursor(query.Cursor)
	if err != nil {
		return DeadLetterListQuery{}, err
	}
	if cursor != nil {
		if offset != 0 {
			return DeadLetterListQuery{}, fmt.Errorf("%w: cursor and offset cannot be combined", ErrInvalidEventListQuery)
		}
		if rawOrder == "" {
			order = cursor.Order
		} else if order != cursor.Order {
			return DeadLetterListQuery{}, fmt.Errorf("%w: cursor order mismatch", ErrInvalidEventListQuery)
		}
	}
	query.Limit = limit
	query.Offset = offset
	query.Order = order
	query.From = from
	query.To = to
	query.cursor = cursor
	return query, nil
}

func normalizeEventListWindow(limit, offset int, order EventListOrder) (int, int, EventListOrder, error) {
	switch {
	case limit < 0:
		return 0, 0, "", fmt.Errorf("%w: limit must be >= 0", ErrInvalidEventListQuery)
	case limit == 0:
		limit = DefaultEventListLimit
	case limit > MaxEventListLimit:
		return 0, 0, "", fmt.Errorf("%w: limit must be <= %d", ErrInvalidEventListQuery, MaxEventListLimit)
	}
	if offset < 0 {
		return 0, 0, "", fmt.Errorf("%w: offset must be >= 0", ErrInvalidEventListQuery)
	}
	if order == "" {
		order = EventListOrderDesc
	}
	if order != EventListOrderAsc && order != EventListOrderDesc {
		return 0, 0, "", fmt.Errorf("%w: order must be asc or desc", ErrInvalidEventListQuery)
	}
	return limit, offset, order, nil
}

func normalizeEventListTimeRange(from, to time.Time) (time.Time, time.Time, error) {
	if !from.IsZero() {
		from = from.UTC()
	}
	if !to.IsZero() {
		to = to.UTC()
	}
	if !from.IsZero() && !to.IsZero() && from.After(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from must be <= to", ErrInvalidEventListQuery)
	}
	return from, to, nil
}

func decodeEventListCursor(raw string) (*eventListCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cursor encoding", ErrInvalidEventListQuery)
	}
	var cursor eventListCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil {
		return nil, fmt.Errorf("%w: invalid cursor payload", ErrInvalidEventListQuery)
	}
	if cursor.Version != 1 {
		return nil, fmt.Errorf("%w: unsupported cursor version", ErrInvalidEventListQuery)
	}
	if cursor.Order != EventListOrderAsc && cursor.Order != EventListOrderDesc {
		return nil, fmt.Errorf("%w: invalid cursor order", ErrInvalidEventListQuery)
	}
	if cursor.Timestamp.IsZero() {
		return nil, fmt.Errorf("%w: cursor timestamp is required", ErrInvalidEventListQuery)
	}
	if cursor.ID == "" {
		return nil, fmt.Errorf("%w: cursor id is required", ErrInvalidEventListQuery)
	}
	cursor.Timestamp = cursor.Timestamp.UTC()
	return &cursor, nil
}

func encodeEventListCursor(cursor eventListCursor) string {
	cursor.Version = 1
	cursor.Timestamp = cursor.Timestamp.UTC()
	body, err := json.Marshal(cursor)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(body)
}

func iterateAcceptedRecords(records []domain.AcceptedEventRecord, order EventListOrder, visit func(domain.AcceptedEventRecord) bool) {
	if order == EventListOrderAsc {
		for _, record := range records {
			if !visit(record) {
				return
			}
		}
		return
	}
	for idx := len(records) - 1; idx >= 0; idx-- {
		if !visit(records[idx]) {
			return
		}
	}
}

func iterateDeadLetterRecords(records []domain.DeadLetterRecord, order EventListOrder, visit func(domain.DeadLetterRecord) bool) {
	if order == EventListOrderAsc {
		for _, record := range records {
			if !visit(record) {
				return
			}
		}
		return
	}
	for idx := len(records) - 1; idx >= 0; idx-- {
		if !visit(records[idx]) {
			return
		}
	}
}

func acceptedEventMatches(record domain.AcceptedEventRecord, query AcceptedEventListQuery) bool {
	if query.DeviceID != "" && record.Event.DeviceID != query.DeviceID {
		return false
	}
	if query.Kind != "" && record.Event.Kind != query.Kind {
		return false
	}
	if query.Source != "" && record.Source != query.Source {
		return false
	}
	return withinTimeRange(record.AcceptedAt, query.From, query.To)
}

func deadLetterMatches(record domain.DeadLetterRecord, query DeadLetterListQuery) bool {
	if query.DeviceID != "" && record.DeviceID != query.DeviceID {
		return false
	}
	if query.Kind != "" && record.Kind != query.Kind {
		return false
	}
	if query.Source != "" && record.Source != query.Source {
		return false
	}
	if query.EventID != "" && record.EventID != query.EventID {
		return false
	}
	return withinTimeRange(record.RecordedAt, query.From, query.To)
}

func withinTimeRange(value, from, to time.Time) bool {
	if !from.IsZero() && value.Before(from) {
		return false
	}
	if !to.IsZero() && value.After(to) {
		return false
	}
	return true
}

func isAcceptedEventAfterCursor(record domain.AcceptedEventRecord, cursor eventListCursor) bool {
	return isRecordAfterCursor(record.AcceptedAt.UTC(), acceptedEventCursorID(record), cursor)
}

func isDeadLetterAfterCursor(record domain.DeadLetterRecord, cursor eventListCursor) bool {
	return isRecordAfterCursor(record.RecordedAt.UTC(), record.ID, cursor)
}

func isRecordAfterCursor(timestamp time.Time, id string, cursor eventListCursor) bool {
	switch cursor.Order {
	case EventListOrderAsc:
		if timestamp.After(cursor.Timestamp) {
			return true
		}
		if timestamp.Equal(cursor.Timestamp) && id > cursor.ID {
			return true
		}
	case EventListOrderDesc:
		if timestamp.Before(cursor.Timestamp) {
			return true
		}
		if timestamp.Equal(cursor.Timestamp) && id < cursor.ID {
			return true
		}
	}
	return false
}

func acceptedEventCursorID(record domain.AcceptedEventRecord) string {
	if record.Event.ID != "" {
		return record.Event.ID
	}
	if record.Event.DeviceID != "" || record.Event.SeqNo > 0 {
		return fmt.Sprintf("%s:%d", record.Event.DeviceID, record.Event.SeqNo)
	}
	return record.AcceptedAt.UTC().Format(time.RFC3339Nano)
}
