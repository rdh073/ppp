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
	events []domain.Event
	err    error
}

func (r *recordingEventProcessor) ProcessEvent(_ context.Context, event domain.Event) error {
	r.events = append(r.events, event)
	return r.err
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
