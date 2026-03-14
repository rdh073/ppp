package domain

import (
	"encoding/json"
	"errors"
	"time"
)

type EventKind string

// ErrEventDropped indicates a device-originated event was intentionally ignored
// (e.g. stale watermark or duplicate idempotency key).
var ErrEventDropped = errors.New("event dropped")

type EventAcceptance string

const (
	EventAcceptanceAccepted  EventAcceptance = "accepted"
	EventAcceptanceStale     EventAcceptance = "stale"
	EventAcceptanceDuplicate EventAcceptance = "duplicate"
)

const (
	EventKindAgentOnline     EventKind = "agent.online"
	EventKindAgentOffline    EventKind = "agent.offline"
	EventKindAgentHeartbeat  EventKind = "agent.heartbeat"
	EventKindUiObservation   EventKind = "ui.observation"
	EventKindToolResult      EventKind = "tool.result"
	EventKindCommandResponse EventKind = "command.response"

	// Device-originated lifecycle events (android-agent → server).
	// These are sent as JSON-RPC notifications (no id) from the agent.
	EventKindActivityCreated       EventKind = "android.activity.created"
	EventKindActivityResumed       EventKind = "android.activity.resumed"
	EventKindScreenChanged         EventKind = "android.screen.changed"
	EventKindNotification          EventKind = "android.notification"
	EventKindAppForeground         EventKind = "android.app.foreground"
	EventKindAccessibilityDisabled EventKind = "android.accessibility.disabled"
)

// IsDeviceOriginated returns true for events sent proactively by the android-agent.
func (k EventKind) IsDeviceOriginated() bool {
	return len(k) > 8 && k[:8] == "android."
}

// Event is the fundamental message that drives workflow transitions.
// SeqNo is the monotonic counter from the android-agent; 0 for internal events.
// ID is the idempotency key: "deviceID:seqNo" for agent events, a UUID for internal events.
type Event struct {
	ID         string
	Kind       EventKind
	DeviceID   DeviceID
	SeqNo      uint64
	OccurredAt time.Time
	Payload    any // concrete type depends on Kind; see payload types below
}

type AcceptedEventRecord struct {
	Event      Event     `json:"event"`
	AcceptedAt time.Time `json:"acceptedAt"`
	Source     string    `json:"source"`
}

type DeadLetterRecord struct {
	ID         string          `json:"id"`
	EventID    string          `json:"eventId,omitempty"`
	Kind       EventKind       `json:"kind,omitempty"`
	DeviceID   DeviceID        `json:"deviceId,omitempty"`
	SeqNo      uint64          `json:"seqNo,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Reason     string          `json:"reason"`
	Source     string          `json:"source"`
	RecordedAt time.Time       `json:"recordedAt"`
}

func NewDeadLetterRecord(event *Event, rawPayload json.RawMessage, reason, source string) DeadLetterRecord {
	record := DeadLetterRecord{
		ID:         "dead-" + newID(),
		Reason:     reason,
		Source:     source,
		RecordedAt: time.Now(),
	}
	if event != nil {
		record.EventID = event.ID
		record.Kind = event.Kind
		record.DeviceID = event.DeviceID
		record.SeqNo = event.SeqNo
		if len(rawPayload) == 0 {
			rawPayload = MarshalEventPayload(*event)
		}
	}
	if len(rawPayload) > 0 {
		record.Payload = append(json.RawMessage(nil), rawPayload...)
	}
	return record
}

func MarshalEventPayload(event Event) json.RawMessage {
	if event.Payload == nil {
		return nil
	}
	switch payload := event.Payload.(type) {
	case json.RawMessage:
		return append(json.RawMessage(nil), payload...)
	case []byte:
		return append(json.RawMessage(nil), payload...)
	}

	raw, err := json.Marshal(event.Payload)
	if err != nil {
		return nil
	}
	return json.RawMessage(raw)
}

// --- payload types ---

// AgentOnlinePayload carries session information when an agent comes online.
type AgentOnlinePayload struct {
	SessionID    SessionID
	Capabilities []Capability
}

// UiObservationPayload carries the normalised UI snapshot from the agent.
type UiObservationPayload struct {
	Snapshot UiSnapshot
}

// ToolResultPayload carries the result of an internal tool call.
type ToolResultPayload struct {
	TaskID    TaskID
	ToolName  string
	Result    []byte // raw JSON
	ErrString string // non-empty on failure
}

// CommandResponsePayload carries a correlated device.* response.
type CommandResponsePayload struct {
	CommandID string
	Success   bool
	Raw       []byte // raw JSON result or error
}
