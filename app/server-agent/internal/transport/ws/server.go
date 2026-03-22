package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

var upgrader = websocket.Upgrader{
	// Accept all origins. Production deployments should restrict this.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// AgentServer is the HTTP handler for /ws/agent.
// It upgrades connections to WebSocket, runs a per-connection read loop,
// and routes inbound messages to either the agent handler, event ingestion,
// or the dispatcher (response correlation).
type AgentServer struct {
	agentHandler *handler.AgentHandler
	eventUC      usecase.EventIngestion
	reg          registry.AgentRegistry
	disp         dispatcher.Dispatcher
	log          *slog.Logger
}

func NewAgentServer(
	h *handler.AgentHandler,
	eventUC usecase.EventIngestion,
	reg registry.AgentRegistry,
	disp dispatcher.Dispatcher,
	log *slog.Logger,
) *AgentServer {
	return &AgentServer{agentHandler: h, eventUC: eventUC, reg: reg, disp: disp, log: log}
}

func (s *AgentServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Error("websocket upgrade failed", "err", err, "remote", r.RemoteAddr)
		return
	}

	conn := newConn(wsConn, s.log, r.RemoteAddr)
	s.log.Info("agent connected", "remote", r.RemoteAddr)
	s.readLoop(r.Context(), conn)
}

func (s *AgentServer) readLoop(ctx context.Context, conn *Conn) {
	defer func() {
		if sessionID := conn.Session(); sessionID != "" {
			deviceID := conn.DeviceID()
			if deviceID == "" {
				if sess, _, ok := s.reg.GetBySession(sessionID); ok {
					deviceID = sess.DeviceID
				}
			}

			// Treat transport close as an implicit disconnect.
			s.agentHandler.HandleTransportDisconnect(context.Background(), deviceID, sessionID)
			if deviceID != "" {
				activeSession, _, stillConnected := s.reg.GetByDevice(deviceID)
				shouldCancel := !stillConnected || activeSession.ID == sessionID
				if shouldCancel {
					s.disp.CancelByDevice(deviceID, "websocket transport disconnected")
				}
			}
			s.log.Info("session removed on disconnect", "sessionId", sessionID, "deviceId", deviceID)
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
			// Inbound request/notification from the agent.
			s.dispatch(ctx, in, conn)
		} else {
			// Inbound response to a device.* command the server sent.
			s.deliverResponse(conn, in)
		}
	}
}

// deliverResponse correlates an agent's device.* response with the pending Dispatch call.
func (s *AgentServer) deliverResponse(conn *Conn, in inbound) {
	if in.ID == "" {
		s.log.Warn("received response with empty id — dropping")
		return
	}

	result := domain.CommandResult{
		CommandID:  in.ID,
		DeviceID:   conn.DeviceID(),
		Success:    in.Error == nil,
		Raw:        in.Result,
		ReceivedAt: time.Now(),
	}
	if in.Error != nil {
		result.Err = &domain.CommandError{Code: in.Error.Code, Message: in.Error.Message}
	}

	s.disp.DeliverResponse(result)
}

func (s *AgentServer) dispatch(ctx context.Context, req inbound, conn *Conn) {
	switch req.Method {
	case "agent.hello":
		s.agentHandler.HandleHello(ctx, req.ID, req.Params, conn, conn.RemoteAddr())
	case "agent.resume":
		s.agentHandler.HandleResume(ctx, req.ID, req.Params, conn, conn.RemoteAddr())
	case "agent.heartbeat":
		s.agentHandler.HandleHeartbeat(ctx, req.ID, req.Params, conn)
	case "agent.disconnect":
		s.agentHandler.HandleDisconnect(ctx, req.ID, req.Params, conn)
	default:
		// Device-originated event notifications (android.*) are routed to
		// the event ingestion use case. They may or may not have an id;
		// we don't send a response for pure notifications (id == "").
		if strings.HasPrefix(req.Method, "android.") {
			deviceID := conn.DeviceID()
			if deviceID == "" {
				s.log.Warn("dropping android event from unregistered connection",
					"method", req.Method)
				return
			}
			if err := s.eventUC.IngestNotification(ctx, deviceID, req.Method, req.Params); err != nil {
				s.log.Warn("event ingestion failed", "method", req.Method, "err", err)
			}
			return
		}
		_ = conn.SendError(req.ID, ErrMethodUnknown, "method not found: "+req.Method)
		s.log.Warn("unknown method", "method", req.Method)
	}
}
