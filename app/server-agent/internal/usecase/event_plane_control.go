package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
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

func (u *EventPlaneControlUseCase) ListAccepted(ctx context.Context) ([]domain.AcceptedEventRecord, error) {
	return u.events.ListAccepted(ctx)
}

func (u *EventPlaneControlUseCase) ListDeadLetters(ctx context.Context) ([]domain.DeadLetterRecord, error) {
	return u.events.ListDeadLetters(ctx)
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
