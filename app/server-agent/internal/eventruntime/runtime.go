package eventruntime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
)

// AcceptedEventProcessor runs workflow logic for events that have already
// passed durable acceptance and ordering checks.
type AcceptedEventProcessor interface {
	ProcessAcceptedEvent(ctx context.Context, e domain.Event) error
}

// Runtime is the event submission boundary used by transport and use cases.
// It accepts events durably, drops stale/duplicate entries, and either
// processes them inline or publishes them to an external wakeup bus.
type Runtime interface {
	ProcessEvent(ctx context.Context, e domain.Event) error
	ReplayAcceptedEvent(ctx context.Context, e domain.Event) error
	RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error
	Start(ctx context.Context) error
}

type InlineRuntime struct {
	events    store.EventPlaneStore
	processor AcceptedEventProcessor
	log       *slog.Logger
	metrics   *telemetry.Registry
}

func NewInlineRuntime(
	events store.EventPlaneStore,
	processor AcceptedEventProcessor,
	log *slog.Logger,
	metrics ...*telemetry.Registry,
) *InlineRuntime {
	registry := telemetry.NewRegistry()
	if len(metrics) > 0 && metrics[0] != nil {
		registry = metrics[0]
	}
	return &InlineRuntime{
		events:    events,
		processor: processor,
		log:       log,
		metrics:   registry,
	}
}

func (r *InlineRuntime) Start(context.Context) error { return nil }

func (r *InlineRuntime) ProcessEvent(ctx context.Context, e domain.Event) error {
	status, _, err := acceptEvent(ctx, r.events, e)
	if err != nil {
		return err
	}
	switch status {
	case domain.EventAcceptanceStale:
		r.log.Debug("dropped stale event", "deviceId", e.DeviceID, "seqNo", e.SeqNo)
		return domain.ErrEventDropped
	case domain.EventAcceptanceDuplicate:
		r.log.Debug("dropped duplicate event", "deviceId", e.DeviceID, "eventId", e.ID)
		return domain.ErrEventDropped
	default:
		return r.processor.ProcessAcceptedEvent(ctx, e)
	}
}

func (r *InlineRuntime) ReplayAcceptedEvent(ctx context.Context, e domain.Event) error {
	return r.processor.ProcessAcceptedEvent(ctx, e)
}

func (r *InlineRuntime) RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error {
	return r.events.RecordDeadLetter(ctx, record)
}

type QueuedRuntime struct {
	events    store.EventPlaneStore
	processor AcceptedEventProcessor
	bus       Bus
	log       *slog.Logger
	metrics   *telemetry.Registry
}

func NewQueuedRuntime(
	events store.EventPlaneStore,
	processor AcceptedEventProcessor,
	bus Bus,
	log *slog.Logger,
	metrics ...*telemetry.Registry,
) *QueuedRuntime {
	registry := telemetry.NewRegistry()
	if len(metrics) > 0 && metrics[0] != nil {
		registry = metrics[0]
	}
	return &QueuedRuntime{
		events:    events,
		processor: processor,
		bus:       bus,
		log:       log,
		metrics:   registry,
	}
}

func (r *QueuedRuntime) Start(ctx context.Context) error {
	return r.bus.Start(ctx, r.processor)
}

func (r *QueuedRuntime) ProcessEvent(ctx context.Context, e domain.Event) error {
	status, accepted, err := acceptEvent(ctx, r.events, e)
	if err != nil {
		return err
	}
	switch status {
	case domain.EventAcceptanceStale:
		r.log.Debug("dropped stale event", "deviceId", e.DeviceID, "seqNo", e.SeqNo)
		return domain.ErrEventDropped
	case domain.EventAcceptanceDuplicate:
		r.log.Debug("dropped duplicate event", "deviceId", e.DeviceID, "eventId", e.ID)
		return domain.ErrEventDropped
	}

	if err := r.bus.PublishAccepted(ctx, accepted); err != nil {
		r.log.Warn("publish accepted event failed; processing inline",
			"eventId", e.ID, "deviceId", e.DeviceID, "err", err)
		return r.processor.ProcessAcceptedEvent(ctx, e)
	}
	if err := r.bus.PublishWakeup(ctx, e); err != nil {
		r.metrics.RecordWakeupFallback(telemetry.WakeupFallbackIngress)
		r.log.Warn("publish workflow wakeup failed; processing inline",
			"eventId", e.ID, "deviceId", e.DeviceID, "err", err)
		return r.processor.ProcessAcceptedEvent(ctx, e)
	}
	return nil
}

func (r *QueuedRuntime) ReplayAcceptedEvent(ctx context.Context, e domain.Event) error {
	if err := r.bus.PublishWakeup(ctx, e); err != nil {
		r.metrics.RecordWakeupFallback(telemetry.WakeupFallbackAcceptedReplay)
		r.log.Warn("publish workflow wakeup replay failed; processing inline",
			"eventId", e.ID, "deviceId", e.DeviceID, "err", err)
		return r.processor.ProcessAcceptedEvent(ctx, e)
	}
	return nil
}

func (r *QueuedRuntime) RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error {
	if err := r.events.RecordDeadLetter(ctx, record); err != nil {
		return err
	}
	if err := r.bus.PublishDeadLetter(ctx, record); err != nil {
		r.log.Warn("publish dead letter failed", "deadLetterId", record.ID, "err", err)
	}
	return nil
}

func acceptEvent(
	ctx context.Context,
	events store.EventPlaneStore,
	e domain.Event,
) (domain.EventAcceptance, domain.AcceptedEventRecord, error) {
	status, err := events.Accept(ctx, e)
	if err != nil {
		return "", domain.AcceptedEventRecord{}, fmt.Errorf("accept event %s: %w", e.ID, err)
	}
	if status != domain.EventAcceptanceAccepted {
		return status, domain.AcceptedEventRecord{}, nil
	}
	return status, domain.AcceptedEventRecord{
		Event:      e,
		AcceptedAt: time.Now(),
		Source:     eventSource(e),
	}, nil
}

func eventSource(event domain.Event) string {
	if event.Kind.IsDeviceOriginated() {
		return "device"
	}
	return "internal"
}
