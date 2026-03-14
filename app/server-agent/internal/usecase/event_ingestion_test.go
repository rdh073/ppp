package usecase_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

type recordingEventProcessor struct {
	events      []domain.Event
	deadLetters []domain.DeadLetterRecord
	err         error
}

func (r *recordingEventProcessor) ProcessEvent(_ context.Context, event domain.Event) error {
	r.events = append(r.events, event)
	return r.err
}

func (r *recordingEventProcessor) RecordDeadLetter(_ context.Context, record domain.DeadLetterRecord) error {
	r.deadLetters = append(r.deadLetters, record)
	return nil
}

type recordingAccessibilityEnabler struct {
	calls []enablerCall
	err   error
}

type enablerCall struct {
	deviceID         domain.DeviceID
	adbSerial        string
	serviceComponent string
}

func (r *recordingAccessibilityEnabler) Enable(
	_ context.Context,
	deviceID domain.DeviceID,
	adbSerial, serviceComponent string,
) error {
	r.calls = append(r.calls, enablerCall{
		deviceID:         deviceID,
		adbSerial:        adbSerial,
		serviceComponent: serviceComponent,
	})
	return r.err
}

func TestIngestNotification_AccessibilityDisabled_TriggersEnabler(t *testing.T) {
	orch := &recordingEventProcessor{}
	enabler := &recordingAccessibilityEnabler{}
	uc := usecase.NewEventIngestion(orch, enabler)

	params, err := json.Marshal(map[string]any{
		"seqNo":            42,
		"adbSerial":        "emulator-5554",
		"serviceComponent": "com.autosdk.agent/.service.AgentAccessibilityService",
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	err = uc.IngestNotification(
		context.Background(),
		domain.DeviceID("dev-1"),
		string(domain.EventKindAccessibilityDisabled),
		params,
	)
	if err != nil {
		t.Fatalf("IngestNotification returned error: %v", err)
	}

	if len(enabler.calls) != 1 {
		t.Fatalf("expected enabler to be called once, got %d", len(enabler.calls))
	}
	call := enabler.calls[0]
	if call.deviceID != "dev-1" {
		t.Fatalf("unexpected deviceID: %q", call.deviceID)
	}
	if call.adbSerial != "emulator-5554" {
		t.Fatalf("unexpected adbSerial: %q", call.adbSerial)
	}
	if call.serviceComponent != "com.autosdk.agent/.service.AgentAccessibilityService" {
		t.Fatalf("unexpected serviceComponent: %q", call.serviceComponent)
	}

	if len(orch.events) != 1 {
		t.Fatalf("expected one event to be processed, got %d", len(orch.events))
	}
	if orch.events[0].Kind != domain.EventKindAccessibilityDisabled {
		t.Fatalf("unexpected event kind: %q", orch.events[0].Kind)
	}
	if orch.events[0].SeqNo != 42 {
		t.Fatalf("unexpected seqNo: %d", orch.events[0].SeqNo)
	}
	if orch.events[0].ID != "dev-1:42" {
		t.Fatalf("unexpected event ID: %q", orch.events[0].ID)
	}
}

func TestIngestNotification_NonAccessibilityEvent_DoesNotTriggerEnabler(t *testing.T) {
	orch := &recordingEventProcessor{}
	enabler := &recordingAccessibilityEnabler{}
	uc := usecase.NewEventIngestion(orch, enabler)

	err := uc.IngestNotification(
		context.Background(),
		domain.DeviceID("dev-2"),
		string(domain.EventKindAppForeground),
		json.RawMessage(`{"seqNo":7}`),
	)
	if err != nil {
		t.Fatalf("IngestNotification returned error: %v", err)
	}

	if len(enabler.calls) != 0 {
		t.Fatalf("expected no enabler call, got %d", len(enabler.calls))
	}
	if len(orch.events) != 1 {
		t.Fatalf("expected one event to be processed, got %d", len(orch.events))
	}
}

func TestIngestNotification_EnablerError_DoesNotBlockEventProcessing(t *testing.T) {
	orch := &recordingEventProcessor{}
	enabler := &recordingAccessibilityEnabler{err: errors.New("boom")}
	uc := usecase.NewEventIngestion(orch, enabler)

	err := uc.IngestNotification(
		context.Background(),
		domain.DeviceID("dev-3"),
		string(domain.EventKindAccessibilityDisabled),
		json.RawMessage(`{"seqNo":8}`),
	)
	if err == nil {
		t.Fatal("expected enabler error")
	}

	if len(orch.events) != 1 {
		t.Fatalf("expected event to still be processed, got %d", len(orch.events))
	}
}

func TestIngestNotification_DroppedByOrchestrator_SkipsSideEffectAndReturnsNil(t *testing.T) {
	orch := &recordingEventProcessor{err: domain.ErrEventDropped}
	enabler := &recordingAccessibilityEnabler{}
	uc := usecase.NewEventIngestion(orch, enabler)

	err := uc.IngestNotification(
		context.Background(),
		domain.DeviceID("dev-4"),
		string(domain.EventKindAccessibilityDisabled),
		json.RawMessage(`{"seqNo":10,"adbSerial":"emulator-5554"}`),
	)
	if err != nil {
		t.Fatalf("expected nil for dropped event, got: %v", err)
	}
	if len(enabler.calls) != 0 {
		t.Fatalf("expected no enabler call for dropped event, got %d", len(enabler.calls))
	}
}

func TestIngestNotification_SeqNoZero_DoesNotTriggerSideEffect(t *testing.T) {
	orch := &recordingEventProcessor{}
	enabler := &recordingAccessibilityEnabler{}
	uc := usecase.NewEventIngestion(orch, enabler)

	err := uc.IngestNotification(
		context.Background(),
		domain.DeviceID("dev-5"),
		string(domain.EventKindAccessibilityDisabled),
		json.RawMessage(`{"seqNo":0,"adbSerial":"emulator-5554"}`),
	)
	if err != nil {
		t.Fatalf("IngestNotification returned error: %v", err)
	}
	if len(orch.events) != 1 {
		t.Fatalf("expected one processed event, got %d", len(orch.events))
	}
	if len(enabler.calls) != 0 {
		t.Fatalf("expected no enabler call for seqNo=0, got %d", len(enabler.calls))
	}
}

// TestIngestNotification_LearnedSerial_UsedWhenAbsent covers the multi-device gap:
// a device sends adbSerial in an earlier event; a later accessibility.disabled event
// omits it; the enabler must still receive the correct serial from the cache.
func TestIngestNotification_LearnedSerial_UsedWhenAbsent(t *testing.T) {
	orch := &recordingEventProcessor{}
	enabler := &recordingAccessibilityEnabler{}
	uc := usecase.NewEventIngestion(orch, enabler)
	ctx := context.Background()
	device := domain.DeviceID("dev-learned")

	// First event: carries adbSerial → server learns it.
	_ = uc.IngestNotification(ctx, device, string(domain.EventKindAppForeground),
		json.RawMessage(`{"seqNo":1,"adbSerial":"emulator-5554"}`))

	// Second event: accessibility disabled, no adbSerial in params.
	err := uc.IngestNotification(ctx, device, string(domain.EventKindAccessibilityDisabled),
		json.RawMessage(`{"seqNo":2}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(enabler.calls) != 1 {
		t.Fatalf("expected 1 enabler call, got %d", len(enabler.calls))
	}
	if enabler.calls[0].adbSerial != "emulator-5554" {
		t.Errorf("expected cached serial emulator-5554, got %q", enabler.calls[0].adbSerial)
	}
}

// TestIngestNotification_MultiDevice_SerialPerDevice ensures the serial cache is
// keyed per device and does not bleed across devices.
func TestIngestNotification_MultiDevice_SerialPerDevice(t *testing.T) {
	orch := &recordingEventProcessor{}
	enabler := &recordingAccessibilityEnabler{}
	uc := usecase.NewEventIngestion(orch, enabler)
	ctx := context.Background()

	devA := domain.DeviceID("dev-A")
	devB := domain.DeviceID("dev-B")

	// Device A registers its serial.
	_ = uc.IngestNotification(ctx, devA, string(domain.EventKindAppForeground),
		json.RawMessage(`{"seqNo":1,"adbSerial":"emulator-5554"}`))

	// Device B registers its own serial.
	_ = uc.IngestNotification(ctx, devB, string(domain.EventKindAppForeground),
		json.RawMessage(`{"seqNo":1,"adbSerial":"emulator-5556"}`))

	// Both send accessibility.disabled without serial.
	_ = uc.IngestNotification(ctx, devA, string(domain.EventKindAccessibilityDisabled),
		json.RawMessage(`{"seqNo":2}`))
	_ = uc.IngestNotification(ctx, devB, string(domain.EventKindAccessibilityDisabled),
		json.RawMessage(`{"seqNo":2}`))

	if len(enabler.calls) != 2 {
		t.Fatalf("expected 2 enabler calls, got %d", len(enabler.calls))
	}

	byDevice := map[domain.DeviceID]string{}
	for _, c := range enabler.calls {
		byDevice[c.deviceID] = c.adbSerial
	}
	if byDevice[devA] != "emulator-5554" {
		t.Errorf("device A: expected emulator-5554, got %q", byDevice[devA])
	}
	if byDevice[devB] != "emulator-5556" {
		t.Errorf("device B: expected emulator-5556, got %q", byDevice[devB])
	}
}

func TestIngestNotification_InvalidJSON_RecordsDeadLetter(t *testing.T) {
	orch := &recordingEventProcessor{}
	enabler := &recordingAccessibilityEnabler{}
	uc := usecase.NewEventIngestion(orch, enabler)

	err := uc.IngestNotification(
		context.Background(),
		domain.DeviceID("dev-6"),
		string(domain.EventKindAccessibilityDisabled),
		json.RawMessage(`{"seqNo":`),
	)
	if err == nil {
		t.Fatal("expected invalid json error")
	}
	if len(orch.events) != 0 {
		t.Fatalf("expected no processed events, got %d", len(orch.events))
	}
	if len(orch.deadLetters) != 1 {
		t.Fatalf("expected one dead letter, got %d", len(orch.deadLetters))
	}
}
