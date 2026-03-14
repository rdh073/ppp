package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	orch         EventProcessor
	autoEnabler  AccessibilityAutoEnabler
	knownSerials sync.Map // domain.DeviceID → string; learned from event params
}

type deadLetterRecorder interface {
	RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error
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
		event := domain.Event{
			Kind:     kind,
			DeviceID: deviceID,
			Payload:  rawParams,
		}
		u.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&event, rawParams, fmt.Sprintf("unexpected method %q", method), "ingestion"))
		return fmt.Errorf("IngestNotification: unexpected method %q", method)
	}

	// Extract optional metadata from params (convention: {"seqNo": N, ...}).
	var meta struct {
		SeqNo            uint64 `json:"seqNo"`
		ADBSerial        string `json:"adbSerial"`
		ServiceComponent string `json:"serviceComponent"`
	}
	if len(rawParams) > 0 {
		if err := json.Unmarshal(rawParams, &meta); err != nil {
			event := domain.Event{
				Kind:     kind,
				DeviceID: deviceID,
				Payload:  rawParams,
			}
			u.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&event, rawParams, fmt.Sprintf("decode params: %v", err), "ingestion"))
			return fmt.Errorf("decode notification params: %w", err)
		}
	}

	// Learn the ADB serial for this device whenever it is present in params.
	// Later events (including android.accessibility.disabled) may omit it,
	// so we resolve it from the cache before calling the auto-enabler.
	if meta.ADBSerial != "" {
		u.knownSerials.Store(deviceID, meta.ADBSerial)
	}
	effectiveSerial := meta.ADBSerial
	if effectiveSerial == "" {
		if v, ok := u.knownSerials.Load(deviceID); ok {
			effectiveSerial = v.(string)
		}
	}

	now := time.Now()
	payloadCopy := make(json.RawMessage, len(rawParams))
	copy(payloadCopy, rawParams)
	event := domain.Event{
		ID:         buildDeviceEventID(deviceID, meta.SeqNo, method, now),
		Kind:       kind,
		DeviceID:   deviceID,
		SeqNo:      meta.SeqNo,
		OccurredAt: now,
		Payload:    payloadCopy,
	}

	processErr := u.orch.ProcessEvent(ctx, event)
	if processErr != nil {
		if errors.Is(processErr, domain.ErrEventDropped) {
			return nil
		}
		return processErr
	}

	return u.maybeEnableAccessibility(
		ctx,
		kind,
		meta.SeqNo,
		deviceID,
		effectiveSerial,
		meta.ServiceComponent,
	)
}

func (u *EventIngestionUseCase) maybeEnableAccessibility(
	ctx context.Context,
	kind domain.EventKind,
	seqNo uint64,
	deviceID domain.DeviceID,
	adbSerial string,
	serviceComponent string,
) error {
	if kind != domain.EventKindAccessibilityDisabled {
		return nil
	}
	if seqNo == 0 {
		// No ordering/idempotency guarantees for seqNo=0 events: keep visibility
		// by ingesting the event, but avoid side-effects.
		return nil
	}

	return u.autoEnabler.Enable(
		ctx,
		deviceID,
		adbSerial,
		serviceComponent,
	)
}

// ForgetDevice removes any cached ADB serial for deviceID.
// Call this when a device disconnects to prevent stale serial reuse.
func (u *EventIngestionUseCase) ForgetDevice(deviceID domain.DeviceID) {
	u.knownSerials.Delete(deviceID)
}

func (u *EventIngestionUseCase) recordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) {
	recorder, ok := u.orch.(deadLetterRecorder)
	if !ok {
		slog.Default().Warn("dead letter recorder not available; ingestion dead letter dropped",
			"eventId", record.EventID, "reason", record.Reason)
		return
	}
	_ = recorder.RecordDeadLetter(ctx, record)
}

func buildDeviceEventID(
	deviceID domain.DeviceID,
	seqNo uint64,
	method string,
	now time.Time,
) string {
	if seqNo > 0 {
		return fmt.Sprintf("%s:%d", deviceID, seqNo)
	}
	return fmt.Sprintf("%s:%s:%d", deviceID, method, now.UnixNano())
}
