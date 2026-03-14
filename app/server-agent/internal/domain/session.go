package domain

import "time"

type SessionID string
type DeviceID string

type Capability struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Session is the server-side representation of a connected android-agent.
// The server is the source of truth for session lifecycle.
type Session struct {
	ID              SessionID
	DeviceID        DeviceID
	AgentInstanceID string
	Capabilities    []Capability
	ConnectedAt     time.Time
	LastHeartbeatAt time.Time
}

func NewSessionID() SessionID {
	return SessionID("sess-" + newID())
}
