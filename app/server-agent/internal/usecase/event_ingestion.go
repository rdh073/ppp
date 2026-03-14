package usecase

import (
	"context"
	"encoding/json"
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
	orch EventProcessor
}

func NewEventIngestion(orch EventProcessor) *EventIngestionUseCase {
	return &EventIngestionUseCase{orch: orch}
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

	return u.orch.ProcessEvent(ctx, event)
}
