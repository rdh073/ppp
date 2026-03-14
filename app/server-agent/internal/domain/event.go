package domain

import "time"

type EventKind string

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
