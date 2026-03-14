package domain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

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
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return SessionID(fmt.Sprintf("sess-%d", time.Now().UnixNano()))
	}
	return SessionID("sess-" + hex.EncodeToString(b))
}
