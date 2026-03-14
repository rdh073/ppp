package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

var upgrader = websocket.Upgrader{
	// Accept all origins. Production deployments should restrict this.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// AgentServer is the HTTP handler for /ws/agent.
// It upgrades connections to WebSocket and runs a read loop per connection.
type AgentServer struct {
	agentHandler *handler.AgentHandler
	reg          *registry.Registry
	log          *slog.Logger
}

func NewAgentServer(h *handler.AgentHandler, reg *registry.Registry, log *slog.Logger) *AgentServer {
	return &AgentServer{agentHandler: h, reg: reg, log: log}
}

func (s *AgentServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Error("websocket upgrade failed", "err", err, "remote", r.RemoteAddr)
		return
	}

	conn := newConn(wsConn, s.log)
	s.log.Info("agent connected", "remote", r.RemoteAddr)
	s.readLoop(r.Context(), conn)
}

func (s *AgentServer) readLoop(ctx context.Context, conn *Conn) {
	defer func() {
		// Clean up the session from the registry when the connection drops.
		if id := conn.Session(); id != "" {
			s.reg.Remove(id)
			s.log.Info("session removed on disconnect", "sessionId", id)
		}
		_ = conn.Close()
	}()

	for {
		_, msg, err := conn.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseNoStatusReceived,
			) {
				s.log.Warn("websocket read error", "err", err)
			}
			return
		}

		var in inbound
		if err := json.Unmarshal(msg, &in); err != nil {
			s.log.Warn("unparseable json-rpc message", "err", err)
			continue
		}

		if in.Method != "" {
			// Incoming request from the agent (agent.hello, agent.resume, etc.)
			s.dispatch(ctx, in, conn)
		}
		// Incoming responses (to device.* commands we sent) are not handled here yet.
	}
}

func (s *AgentServer) dispatch(ctx context.Context, req inbound, conn *Conn) {
	switch req.Method {
	case "agent.hello":
		s.agentHandler.HandleHello(ctx, req.ID, req.Params, conn)
	case "agent.resume":
		s.agentHandler.HandleResume(ctx, req.ID, req.Params, conn)
	case "agent.heartbeat":
		s.agentHandler.HandleHeartbeat(ctx, req.ID, req.Params, conn)
	case "agent.disconnect":
		s.agentHandler.HandleDisconnect(ctx, req.ID, req.Params, conn)
	default:
		_ = conn.SendError(req.ID, ErrMethodUnknown, "method not found: "+req.Method)
		s.log.Warn("unknown method", "method", req.Method)
	}
}
