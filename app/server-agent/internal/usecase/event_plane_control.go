package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
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

type acceptedEventReplayer interface {
	ReplayAcceptedEvent(ctx context.Context, event domain.Event) error
}

type notificationReplayer interface {
	IngestNotification(ctx context.Context, deviceID domain.DeviceID, method string, rawParams json.RawMessage) error
}

// EventPlaneControlUseCase exposes durable accepted-event and dead-letter
// inspection plus operator-triggered replay.
type EventPlaneControlUseCase struct {
	events             store.EventPlaneStore
	acceptedReplayer   acceptedEventReplayer
	notificationIngest notificationReplayer
	log                *slog.Logger
}

type AcceptedEventListQuery struct {
	DeviceID domain.DeviceID
	Kind     domain.EventKind
	Source   string
	Order    EventListOrder
	Limit    int
	Offset   int
}

type AcceptedEventPage struct {
	Items   []domain.AcceptedEventRecord `json:"items"`
	Total   int                          `json:"total"`
	Offset  int                          `json:"offset"`
	Limit   int                          `json:"limit"`
	HasMore bool                         `json:"hasMore"`
}

type DeadLetterListQuery struct {
	DeviceID domain.DeviceID
	Kind     domain.EventKind
	Source   string
	EventID  string
	Order    EventListOrder
	Limit    int
	Offset   int
}

type DeadLetterPage struct {
	Items   []domain.DeadLetterRecord `json:"items"`
	Total   int                       `json:"total"`
	Offset  int                       `json:"offset"`
	Limit   int                       `json:"limit"`
	HasMore bool                      `json:"hasMore"`
}

func NewEventPlaneControl(
	events store.EventPlaneStore,
	accepted acceptedEventReplayer,
	notifications notificationReplayer,
	log *slog.Logger,
) *EventPlaneControlUseCase {
	return &EventPlaneControlUseCase{
		events:             events,
		acceptedReplayer:   accepted,
		notificationIngest: notifications,
		log:                log,
	}
}

func (u *EventPlaneControlUseCase) ListAccepted(
	ctx context.Context,
	query AcceptedEventListQuery,
) (AcceptedEventPage, error) {
	query, err := normalizeAcceptedEventListQuery(query)
	if err != nil {
		return AcceptedEventPage{}, err
	}

	records, err := u.events.ListAccepted(ctx)
	if err != nil {
		return AcceptedEventPage{}, err
	}

	filtered := make([]domain.AcceptedEventRecord, 0, len(records))
	appendRecord := func(record domain.AcceptedEventRecord) {
		if query.DeviceID != "" && record.Event.DeviceID != query.DeviceID {
			return
		}
		if query.Kind != "" && record.Event.Kind != query.Kind {
			return
		}
		if query.Source != "" && record.Source != query.Source {
			return
		}
		filtered = append(filtered, record)
	}

	if query.Order == EventListOrderAsc {
		for _, record := range records {
			appendRecord(record)
		}
	} else {
		for idx := len(records) - 1; idx >= 0; idx-- {
			appendRecord(records[idx])
		}
	}

	return buildAcceptedEventPage(filtered, query), nil
}

func (u *EventPlaneControlUseCase) ListDeadLetters(
	ctx context.Context,
	query DeadLetterListQuery,
) (DeadLetterPage, error) {
	query, err := normalizeDeadLetterListQuery(query)
	if err != nil {
		return DeadLetterPage{}, err
	}

	records, err := u.events.ListDeadLetters(ctx)
	if err != nil {
		return DeadLetterPage{}, err
	}

	filtered := make([]domain.DeadLetterRecord, 0, len(records))
	appendRecord := func(record domain.DeadLetterRecord) {
		if query.DeviceID != "" && record.DeviceID != query.DeviceID {
			return
		}
		if query.Kind != "" && record.Kind != query.Kind {
			return
		}
		if query.Source != "" && record.Source != query.Source {
			return
		}
		if query.EventID != "" && record.EventID != query.EventID {
			return
		}
		filtered = append(filtered, record)
	}

	if query.Order == EventListOrderAsc {
		for _, record := range records {
			appendRecord(record)
		}
	} else {
		for idx := len(records) - 1; idx >= 0; idx-- {
			appendRecord(records[idx])
		}
	}

	return buildDeadLetterPage(filtered, query), nil
}

func (u *EventPlaneControlUseCase) GetAccepted(ctx context.Context, eventID string) (*domain.AcceptedEventRecord, error) {
	records, err := u.events.ListAccepted(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.Event.ID == eventID {
			cp := record
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("%w: accepted event %s", store.ErrNotFound, eventID)
}

func (u *EventPlaneControlUseCase) GetDeadLetter(ctx context.Context, deadLetterID string) (*domain.DeadLetterRecord, error) {
	records, err := u.events.ListDeadLetters(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.ID == deadLetterID {
			cp := record
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("%w: dead letter %s", store.ErrNotFound, deadLetterID)
}

func (u *EventPlaneControlUseCase) ReplayAccepted(ctx context.Context, eventID string) error {
	record, err := u.GetAccepted(ctx, eventID)
	if err != nil {
		return err
	}
	u.log.Info("replay accepted event", "eventId", eventID, "deviceId", record.Event.DeviceID)
	return u.acceptedReplayer.ReplayAcceptedEvent(ctx, cloneEvent(record.Event, time.Now()))
}

func (u *EventPlaneControlUseCase) ReplayDeadLetter(ctx context.Context, deadLetterID string) error {
	record, err := u.GetDeadLetter(ctx, deadLetterID)
	if err != nil {
		return err
	}

	switch record.Source {
	case "ingestion":
		if !record.Kind.IsDeviceOriginated() || record.DeviceID == "" {
			return fmt.Errorf("dead letter %s is not replayable through ingestion", deadLetterID)
		}
		u.log.Info("replay dead letter through ingestion",
			"deadLetterId", record.ID,
			"deviceId", record.DeviceID,
			"kind", record.Kind,
		)
		return u.notificationIngest.IngestNotification(ctx, record.DeviceID, string(record.Kind), record.Payload)
	default:
		event, err := eventFromDeadLetter(*record)
		if err != nil {
			return err
		}
		u.log.Info("replay dead letter through accepted-event path",
			"deadLetterId", record.ID,
			"eventId", event.ID,
			"deviceId", event.DeviceID,
			"kind", event.Kind,
		)
		return u.acceptedReplayer.ReplayAcceptedEvent(ctx, event)
	}
}

func eventFromDeadLetter(record domain.DeadLetterRecord) (domain.Event, error) {
	if record.Kind == "" {
		return domain.Event{}, fmt.Errorf("dead letter %s has no event kind", record.ID)
	}
	eventID := record.EventID
	if eventID == "" && record.DeviceID != "" && record.SeqNo > 0 {
		eventID = fmt.Sprintf("%s:%d", record.DeviceID, record.SeqNo)
	}
	if eventID == "" {
		eventID = fmt.Sprintf("replay:%s", record.ID)
	}
	return domain.Event{
		ID:         eventID,
		Kind:       record.Kind,
		DeviceID:   record.DeviceID,
		SeqNo:      record.SeqNo,
		OccurredAt: time.Now(),
		Payload:    append(json.RawMessage(nil), record.Payload...),
	}, nil
}

func cloneEvent(event domain.Event, occurredAt time.Time) domain.Event {
	cloned := domain.Event{
		ID:         event.ID,
		Kind:       event.Kind,
		DeviceID:   event.DeviceID,
		SeqNo:      event.SeqNo,
		OccurredAt: occurredAt,
	}
	if raw := domain.MarshalEventPayload(event); len(raw) > 0 {
		cloned.Payload = raw
	} else {
		cloned.Payload = event.Payload
	}
	return cloned
}

func normalizeAcceptedEventListQuery(query AcceptedEventListQuery) (AcceptedEventListQuery, error) {
	limit, offset, order, err := normalizeEventListWindow(query.Limit, query.Offset, query.Order)
	if err != nil {
		return AcceptedEventListQuery{}, err
	}
	query.Limit = limit
	query.Offset = offset
	query.Order = order
	return query, nil
}

func normalizeDeadLetterListQuery(query DeadLetterListQuery) (DeadLetterListQuery, error) {
	limit, offset, order, err := normalizeEventListWindow(query.Limit, query.Offset, query.Order)
	if err != nil {
		return DeadLetterListQuery{}, err
	}
	query.Limit = limit
	query.Offset = offset
	query.Order = order
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

func buildAcceptedEventPage(records []domain.AcceptedEventRecord, query AcceptedEventListQuery) AcceptedEventPage {
	items, hasMore := paginateAcceptedEventRecords(records, query.Offset, query.Limit)
	return AcceptedEventPage{
		Items:   items,
		Total:   len(records),
		Offset:  query.Offset,
		Limit:   query.Limit,
		HasMore: hasMore,
	}
}

func buildDeadLetterPage(records []domain.DeadLetterRecord, query DeadLetterListQuery) DeadLetterPage {
	items, hasMore := paginateDeadLetterRecords(records, query.Offset, query.Limit)
	return DeadLetterPage{
		Items:   items,
		Total:   len(records),
		Offset:  query.Offset,
		Limit:   query.Limit,
		HasMore: hasMore,
	}
}

func paginateAcceptedEventRecords(
	records []domain.AcceptedEventRecord,
	offset int,
	limit int,
) ([]domain.AcceptedEventRecord, bool) {
	if offset >= len(records) {
		return []domain.AcceptedEventRecord{}, false
	}
	end := offset + limit
	if end > len(records) {
		end = len(records)
	}
	items := append([]domain.AcceptedEventRecord(nil), records[offset:end]...)
	return items, end < len(records)
}

func paginateDeadLetterRecords(
	records []domain.DeadLetterRecord,
	offset int,
	limit int,
) ([]domain.DeadLetterRecord, bool) {
	if offset >= len(records) {
		return []domain.DeadLetterRecord{}, false
	}
	end := offset + limit
	if end > len(records) {
		end = len(records)
	}
	items := append([]domain.DeadLetterRecord(nil), records[offset:end]...)
	return items, end < len(records)
}
