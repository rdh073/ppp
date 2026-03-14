package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// Conn wraps a gorilla WebSocket connection and implements handler.Conn.
// It is safe for concurrent writes from multiple goroutines.
type Conn struct {
	ws        *websocket.Conn
	writeMu   sync.Mutex
	log       *slog.Logger
	sessionID atomic.Pointer[domain.SessionID]
}

func newConn(ws *websocket.Conn, log *slog.Logger) *Conn {
	return &Conn{ws: ws, log: log}
}

// SetSession records which session this connection belongs to.
// Called by the handler after a successful hello/resume.
func (c *Conn) SetSession(id domain.SessionID) {
	c.sessionID.Store(&id)
}

// Session returns the current session ID, or empty string if not yet registered.
func (c *Conn) Session() domain.SessionID {
	if p := c.sessionID.Load(); p != nil {
		return *p
	}
	return ""
}

// SendRequest sends a JSON-RPC 2.0 request from the server to the agent.
func (c *Conn) SendRequest(id, method string, params any) error {
	return c.writeJSON(rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  mustMarshal(params),
	})
}

// SendSuccess sends a JSON-RPC 2.0 success response.
func (c *Conn) SendSuccess(id string, result any) error {
	return c.writeJSON(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

// SendError sends a JSON-RPC 2.0 error response.
func (c *Conn) SendError(id string, code int, message string) error {
	return c.writeJSON(rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

// Close terminates the underlying WebSocket connection.
func (c *Conn) Close() error {
	return c.ws.Close()
}

func (c *Conn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteJSON(v)
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}
