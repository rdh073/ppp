package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

var ErrInvalidEventListQuery = store.ErrInvalidEventListQuery

const (
	DefaultEventListLimit = store.DefaultEventListLimit
	MaxEventListLimit     = store.MaxEventListLimit
)

type EventListOrder = store.EventListOrder

const (
	EventListOrderAsc  = store.EventListOrderAsc
	EventListOrderDesc = store.EventListOrderDesc
)

type AcceptedEventListQuery = store.AcceptedEventListQuery
type AcceptedEventPage = store.AcceptedEventPage
type DeadLetterListQuery = store.DeadLetterListQuery
type DeadLetterPage = store.DeadLetterPage

type AcceptedPayloadPreview struct {
	EventID     string           `json:"eventId"`
	Kind        domain.EventKind `json:"kind"`
	DeviceID    domain.DeviceID  `json:"deviceId"`
	SizeBytes   int              `json:"sizeBytes"`
	Truncated   bool             `json:"truncated"`
	PayloadText string           `json:"payloadText"`
}

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
	metrics            *telemetry.Registry
}

func NewEventPlaneControl(
	events store.EventPlaneStore,
	accepted acceptedEventReplayer,
	notifications notificationReplayer,
	log *slog.Logger,
	metrics ...*telemetry.Registry,
) *EventPlaneControlUseCase {
	registry := telemetry.NewRegistry()
	if len(metrics) > 0 && metrics[0] != nil {
		registry = metrics[0]
	}
	return &EventPlaneControlUseCase{
		events:             events,
		acceptedReplayer:   accepted,
		notificationIngest: notifications,
		log:                log,
		metrics:            registry,
	}
}

func (u *EventPlaneControlUseCase) ListAccepted(
	ctx context.Context,
	query AcceptedEventListQuery,
) (AcceptedEventPage, error) {
	return u.events.QueryAccepted(ctx, query)
}

func (u *EventPlaneControlUseCase) ListDeadLetters(
	ctx context.Context,
	query DeadLetterListQuery,
) (DeadLetterPage, error) {
	return u.events.QueryDeadLetters(ctx, query)
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

func (u *EventPlaneControlUseCase) GetAcceptedPayload(ctx context.Context, eventID string, maxBytes int) (*AcceptedPayloadPreview, error) {
	if maxBytes <= 0 {
		maxBytes = 16 * 1024
	}
	if maxBytes > 256*1024 {
		maxBytes = 256 * 1024
	}

	record, err := u.GetAccepted(ctx, eventID)
	if err != nil {
		return nil, err
	}
	raw := domain.MarshalEventPayload(record.Event)
	payload := string(raw)
	truncated := false
	if len(raw) > maxBytes {
		payload = string(raw[:maxBytes])
		truncated = true
	}
	return &AcceptedPayloadPreview{
		EventID:     record.Event.ID,
		Kind:        record.Event.Kind,
		DeviceID:    record.Event.DeviceID,
		SizeBytes:   len(raw),
		Truncated:   truncated,
		PayloadText: payload,
	}, nil
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
	u.metrics.RecordReplay(telemetry.ReplayPathAccepted, telemetry.ReplayOutcomeAttempted)
	u.log.Info("replay accepted event", "eventId", eventID, "deviceId", record.Event.DeviceID)
	if err := u.acceptedReplayer.ReplayAcceptedEvent(ctx, cloneEvent(record.Event, time.Now())); err != nil {
		u.metrics.RecordReplay(telemetry.ReplayPathAccepted, telemetry.ReplayOutcomeFailed)
		return err
	}
	u.metrics.RecordReplay(telemetry.ReplayPathAccepted, telemetry.ReplayOutcomeSucceeded)
	return nil
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
		u.metrics.RecordReplay(telemetry.ReplayPathDeadLetterIngestion, telemetry.ReplayOutcomeAttempted)
		u.log.Info("replay dead letter through ingestion",
			"deadLetterId", record.ID,
			"deviceId", record.DeviceID,
			"kind", record.Kind,
		)
		if err := u.notificationIngest.IngestNotification(ctx, record.DeviceID, string(record.Kind), record.Payload); err != nil {
			u.metrics.RecordReplay(telemetry.ReplayPathDeadLetterIngestion, telemetry.ReplayOutcomeFailed)
			return err
		}
		u.metrics.RecordReplay(telemetry.ReplayPathDeadLetterIngestion, telemetry.ReplayOutcomeSucceeded)
		return nil
	default:
		event, err := eventFromDeadLetter(*record)
		if err != nil {
			return err
		}
		u.metrics.RecordReplay(telemetry.ReplayPathDeadLetterAccepted, telemetry.ReplayOutcomeAttempted)
		u.log.Info("replay dead letter through accepted-event path",
			"deadLetterId", record.ID,
			"eventId", event.ID,
			"deviceId", event.DeviceID,
			"kind", event.Kind,
		)
		if err := u.acceptedReplayer.ReplayAcceptedEvent(ctx, event); err != nil {
			u.metrics.RecordReplay(telemetry.ReplayPathDeadLetterAccepted, telemetry.ReplayOutcomeFailed)
			return err
		}
		u.metrics.RecordReplay(telemetry.ReplayPathDeadLetterAccepted, telemetry.ReplayOutcomeSucceeded)
		return nil
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
