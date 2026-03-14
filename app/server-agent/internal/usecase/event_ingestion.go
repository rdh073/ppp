package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// EventIngestionUseCase converts device-originated JSON-RPC notifications into
// domain.Events and feeds them into the orchestrator.
//
// Device events arrive as JSON-RPC notifications (method set, id absent) from
// the android-agent. They carry a monotonic SeqNo in their params so the
// orchestrator's watermark and dedup logic can order and deduplicate them.
type EventIngestionUseCase struct {
	orch        EventProcessor
	autoEnabler AccessibilityAutoEnabler
}

func NewEventIngestion(orch EventProcessor, autoEnabler ...AccessibilityAutoEnabler) *EventIngestionUseCase {
	enabler := AccessibilityAutoEnabler(noopAccessibilityAutoEnabler{})
	if len(autoEnabler) > 0 && autoEnabler[0] != nil {
		enabler = autoEnabler[0]
	}
	return &EventIngestionUseCase{
		orch:        orch,
		autoEnabler: enabler,
	}
}

// IngestNotification converts an android-agent JSON-RPC notification into a
// domain.Event and processes it. method must be a known android.* method.
func (u *EventIngestionUseCase) IngestNotification(
	ctx context.Context,
	deviceID domain.DeviceID,
	method string,
	rawParams json.RawMessage,
) error {
	kind := domain.EventKind(method)
	if !kind.IsDeviceOriginated() {
		return fmt.Errorf("IngestNotification: unexpected method %q", method)
	}

	enableErr := u.maybeEnableAccessibility(ctx, kind, deviceID, rawParams)

	// Extract optional seqNo from params (convention: {"seqNo": N, ...}).
	var meta struct {
		SeqNo uint64 `json:"seqNo"`
	}
	_ = json.Unmarshal(rawParams, &meta) // best-effort; zero seqNo is valid

	now := time.Now()
	event := domain.Event{
		ID:         fmt.Sprintf("%s:%s:%d", deviceID, method, now.UnixNano()),
		Kind:       kind,
		DeviceID:   deviceID,
		SeqNo:      meta.SeqNo,
		OccurredAt: now,
		Payload:    rawParams,
	}

	processErr := u.orch.ProcessEvent(ctx, event)
	return errors.Join(processErr, enableErr)
}

func (u *EventIngestionUseCase) maybeEnableAccessibility(
	ctx context.Context,
	kind domain.EventKind,
	deviceID domain.DeviceID,
	rawParams json.RawMessage,
) error {
	if kind != domain.EventKindAccessibilityDisabled {
		return nil
	}

	var payload struct {
		ADBSerial        string `json:"adbSerial"`
		ServiceComponent string `json:"serviceComponent"`
	}
	_ = json.Unmarshal(rawParams, &payload)

	return u.autoEnabler.Enable(
		ctx,
		deviceID,
		payload.ADBSerial,
		payload.ServiceComponent,
	)
}
