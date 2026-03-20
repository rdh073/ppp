package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

type sessionInfo struct {
	sessionID domain.SessionID
	deviceID  domain.DeviceID
}

// Conn wraps a gorilla WebSocket connection and implements handler.Conn.
// It is safe for concurrent writes from multiple goroutines.
type Conn struct {
	ws         *websocket.Conn
	writeMu    sync.Mutex
	log        *slog.Logger
	info       atomic.Pointer[sessionInfo]
	remoteAddr string
}

func newConn(ws *websocket.Conn, log *slog.Logger, remoteAddr string) *Conn {
	return &Conn{ws: ws, log: log, remoteAddr: remoteAddr}
}

// RemoteAddr returns the network address of the connected agent (host:port).
func (c *Conn) RemoteAddr() string { return c.remoteAddr }

// SetSession records which session this connection belongs to.
// DeviceID is obtained from the registry via the session after Hello/Resume.
// It is called by the handler with both IDs so deliverResponse can correlate responses.
func (c *Conn) SetSession(id domain.SessionID) {
	// DeviceID is set separately via SetDevice; here we just store the session ID.
	cur := c.info.Load()
	var devID domain.DeviceID
	if cur != nil {
		devID = cur.deviceID
	}
	c.info.Store(&sessionInfo{sessionID: id, deviceID: devID})
}

// SetDevice records the device ID associated with this connection.
// Called by the handler after a successful Hello/Resume.
func (c *Conn) SetDevice(id domain.DeviceID) {
	cur := c.info.Load()
	var sessID domain.SessionID
	if cur != nil {
		sessID = cur.sessionID
	}
	c.info.Store(&sessionInfo{sessionID: sessID, deviceID: id})
}

// Session returns the current session ID, or empty if not yet registered.
func (c *Conn) Session() domain.SessionID {
	if p := c.info.Load(); p != nil {
		return p.sessionID
	}
	return ""
}

// DeviceID returns the device ID associated with this connection, or empty if unknown.
func (c *Conn) DeviceID() domain.DeviceID {
	if p := c.info.Load(); p != nil {
		return p.deviceID
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
