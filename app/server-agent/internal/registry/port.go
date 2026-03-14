package registry

import "github.com/autosdk/ppp/server-agent/internal/domain"

// AgentRegistry is the interface upper layers use to look up active agent connections.
// The concrete *Registry satisfies it; a future Redis-backed implementation would too.
type AgentRegistry interface {
	Add(session *domain.Session, conn Sender) error
	Remove(id domain.SessionID)
	GetBySession(id domain.SessionID) (*domain.Session, Sender, bool)
	GetByDevice(id domain.DeviceID) (*domain.Session, Sender, bool)
}
