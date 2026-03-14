package eventruntime_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/eventruntime"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type recordingProcessor struct {
	events []domain.Event
	err    error
}

func (p *recordingProcessor) ProcessAcceptedEvent(_ context.Context, e domain.Event) error {
	p.events = append(p.events, e)
	return p.err
}

type fakeBus struct {
	accepted       []domain.AcceptedEventRecord
	wakeups        []domain.Event
	deadLetters    []domain.DeadLetterRecord
	acceptedErr    error
	wakeupErr      error
	deadLetterErr  error
	startCalled    bool
	startProcessor eventruntime.AcceptedEventProcessor
	startErr       error
}

func (b *fakeBus) PublishAccepted(_ context.Context, record domain.AcceptedEventRecord) error {
	b.accepted = append(b.accepted, record)
	return b.acceptedErr
}

func (b *fakeBus) PublishWakeup(_ context.Context, event domain.Event) error {
	b.wakeups = append(b.wakeups, event)
	return b.wakeupErr
}

func (b *fakeBus) PublishDeadLetter(_ context.Context, record domain.DeadLetterRecord) error {
	b.deadLetters = append(b.deadLetters, record)
	return b.deadLetterErr
}

func (b *fakeBus) Start(_ context.Context, processor eventruntime.AcceptedEventProcessor) error {
	b.startCalled = true
	b.startProcessor = processor
	return b.startErr
}

func newLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newEvent(deviceID domain.DeviceID, seqNo uint64) domain.Event {
	return domain.Event{
		ID:         string(deviceID) + ":" + time.Now().Format("150405.000000000"),
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   deviceID,
		SeqNo:      seqNo,
		OccurredAt: time.Now(),
	}
}

func TestInlineRuntime_ProcessesAcceptedEvent(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	processor := &recordingProcessor{}
	runtime := eventruntime.NewInlineRuntime(events, processor, newLog())

	event := newEvent("dev-inline", 1)
	if err := runtime.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(processor.events) != 1 {
		t.Fatalf("expected one processed event, got %d", len(processor.events))
	}
	if err := runtime.ProcessEvent(context.Background(), event); !errors.Is(err, domain.ErrEventDropped) {
		t.Fatalf("expected dropped duplicate, got %v", err)
	}
	if len(processor.events) != 1 {
		t.Fatalf("expected no extra processing on duplicate, got %d", len(processor.events))
	}
}

func TestQueuedRuntime_StartAndPublish(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	processor := &recordingProcessor{}
	bus := &fakeBus{}
	runtime := eventruntime.NewQueuedRuntime(events, processor, bus, newLog())

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !bus.startCalled || bus.startProcessor == nil {
		t.Fatal("expected bus Start to be called with processor")
	}

	event := newEvent("dev-queued", 2)
	if err := runtime.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(bus.accepted) != 1 {
		t.Fatalf("expected one accepted publish, got %d", len(bus.accepted))
	}
	if len(bus.wakeups) != 1 {
		t.Fatalf("expected one wakeup publish, got %d", len(bus.wakeups))
	}
	if len(processor.events) != 0 {
		t.Fatalf("expected no inline processing when bus publish succeeds, got %d", len(processor.events))
	}
}

func TestQueuedRuntime_FallsBackInlineWhenWakeupPublishFails(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	processor := &recordingProcessor{}
	bus := &fakeBus{wakeupErr: errors.New("redis unavailable")}
	runtime := eventruntime.NewQueuedRuntime(events, processor, bus, newLog())

	event := newEvent("dev-fallback", 3)
	if err := runtime.ProcessEvent(context.Background(), event); err != nil {
		t.Fatalf("ProcessEvent: %v", err)
	}
	if len(bus.accepted) != 1 {
		t.Fatalf("expected accepted publish before fallback, got %d", len(bus.accepted))
	}
	if len(processor.events) != 1 {
		t.Fatalf("expected inline fallback processing, got %d", len(processor.events))
	}
}

func TestQueuedRuntime_RecordDeadLetter_PersistsAndPublishes(t *testing.T) {
	events := store.NewMemoryEventPlaneStore()
	processor := &recordingProcessor{}
	bus := &fakeBus{}
	runtime := eventruntime.NewQueuedRuntime(events, processor, bus, newLog())

	record := domain.NewDeadLetterRecord(&domain.Event{
		ID:         "dead-source",
		Kind:       domain.EventKindAccessibilityDisabled,
		DeviceID:   "dev-dead",
		SeqNo:      7,
		OccurredAt: time.Now(),
	}, nil, "boom", "test")

	if err := runtime.RecordDeadLetter(context.Background(), record); err != nil {
		t.Fatalf("RecordDeadLetter: %v", err)
	}

	deadLetters, err := events.ListDeadLetters(context.Background())
	if err != nil {
		t.Fatalf("ListDeadLetters: %v", err)
	}
	if len(deadLetters) != 1 {
		t.Fatalf("expected one persisted dead letter, got %d", len(deadLetters))
	}
	if len(bus.deadLetters) != 1 {
		t.Fatalf("expected one published dead letter, got %d", len(bus.deadLetters))
	}
}
