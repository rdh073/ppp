package registry

import (
	"sync"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// Sender is the server's handle to a connected android-agent.
// It is implemented by the WebSocket connection in the transport layer.
type Sender interface {
	// SendRequest sends a JSON-RPC request from the server to the agent (e.g. device.execute).
	SendRequest(id, method string, params any) error
	// SendSuccess sends a JSON-RPC success response.
	SendSuccess(id string, result any) error
	// SendError sends a JSON-RPC error response.
	SendError(id string, code int, message string) error
	// Close terminates the underlying connection.
	Close() error
}

type entry struct {
	session *domain.Session
	conn    Sender
}

// Registry is an in-memory store mapping sessions and devices to their active connections.
// It is safe for concurrent use.
type Registry struct {
	mu        sync.RWMutex
	bySession map[domain.SessionID]*entry
	byDevice  map[domain.DeviceID]*entry
}

func New() *Registry {
	return &Registry{
		bySession: make(map[domain.SessionID]*entry),
		byDevice:  make(map[domain.DeviceID]*entry),
	}
}

func (r *Registry) Add(session *domain.Session, conn Sender) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := &entry{session: session, conn: conn}
	r.bySession[session.ID] = e
	r.byDevice[session.DeviceID] = e
}

func (r *Registry) Remove(id domain.SessionID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.bySession[id]
	if !ok {
		return
	}
	delete(r.byDevice, e.session.DeviceID)
	delete(r.bySession, id)
}

func (r *Registry) GetBySession(id domain.SessionID) (*domain.Session, Sender, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.bySession[id]
	if !ok {
		return nil, nil, false
	}
	return e.session, e.conn, true
}

func (r *Registry) GetByDevice(id domain.DeviceID) (*domain.Session, Sender, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.byDevice[id]
	if !ok {
		return nil, nil, false
	}
	return e.session, e.conn, true
}
