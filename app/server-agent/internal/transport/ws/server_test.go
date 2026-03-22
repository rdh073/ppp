package ws_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autosdk/ppp/server-agent/internal/devicectrl"
	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/eventing"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	ws "github.com/autosdk/ppp/server-agent/internal/transport/ws"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflowruntime"
)

// observeTerminalEngine builds a workflow engine with a "default" workflow that:
//   - fires on any event
//   - dispatches device.observe (so the WebSocket round-trip actually happens)
//   - routes to terminal on success or failure
//
// This lets the E2E test verify that CreateTask fires an observe command over
// WebSocket and the task completes after the agent responds.
func observeTerminalEngine(disp dispatcher.Dispatcher) *workflow.Engine {
	def := &domain.WorkflowDef{
		Name:  "default",
		Entry: "start",
		Steps: map[string]domain.StepDef{
			"start": {
				Trigger:   domain.EventMatch{},
				Action:    &domain.ActionDef{Kind: domain.ActionKindObserve},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), def.Name, def)
	return workflow.NewEngine(mem, disp)
}

// buildTestServer wires all components and returns an httptest.Server.
// The mux exposes /ws/agent and /tasks exactly as main.go does.
func buildTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	reg := registry.New()
	taskStore := store.NewMemoryTaskStore()
	stateStore := store.NewMemoryWorkflowStateStore()
	disp := dispatcher.NewMemoryDispatcher(reg, nil)
	engine := observeTerminalEngine(disp)
	orch := orchestrator.New(taskStore, stateStore, engine, log)
	lifecycleUC := devicectrl.NewAgentLifecycle(reg, orch, nil, nil, log)
	taskUC := workflowruntime.NewTaskControl(taskStore, stateStore, orch, reg, log)
	eventUC := eventing.NewEventIngestion(orch)
	agentHandler := handler.NewAgentHandler(lifecycleUC, log)
	taskHandler := handler.NewTaskHandler(taskUC, log)
	agentServer := ws.NewAgentServer(agentHandler, eventUC, reg, disp, log)

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", agentServer)
	mux.Handle("/tasks", taskHandler)
	mux.Handle("/tasks/", taskHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func buildTestServerWithProcessor(t *testing.T, proc eventing.EventProcessor) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	reg := registry.New()
	taskStore := store.NewMemoryTaskStore()
	stateStore := store.NewMemoryWorkflowStateStore()
	disp := dispatcher.NewMemoryDispatcher(reg, nil)
	engine := observeTerminalEngine(disp)
	orch := orchestrator.New(taskStore, stateStore, engine, log)
	lifecycleUC := devicectrl.NewAgentLifecycle(reg, orch, nil, nil, log)
	taskUC := workflowruntime.NewTaskControl(taskStore, stateStore, orch, reg, log)
	eventUC := eventing.NewEventIngestion(proc)
	agentHandler := handler.NewAgentHandler(lifecycleUC, log)
	taskHandler := handler.NewTaskHandler(taskUC, log)
	agentServer := ws.NewAgentServer(agentHandler, eventUC, reg, disp, log)

	mux := http.NewServeMux()
	mux.Handle("/ws/agent", agentServer)
	mux.Handle("/tasks", taskHandler)
	mux.Handle("/tasks/", taskHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// wsURL converts an http:// URL to ws://.
func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

// dialWS opens a WebSocket connection to /ws/agent.
func dialWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "/ws/agent"), nil)
	if err != nil {
		t.Fatalf("WebSocket dial failed: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// sendJSON writes a JSON-RPC message over the WebSocket.
func sendJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
}

// readJSON reads one JSON-RPC message from the WebSocket.
func readJSON(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(msg, &out); err != nil {
		t.Fatalf("unmarshal message: %v: raw=%s", err, msg)
	}
	return out
}

// TestE2E_AgentHelloThenTaskDispatchesObserve is the phase-9 smoke test.
//
// Flow:
//  1. agent.hello → server registers session, responds with sessionId
//  2. POST /tasks?deviceId=... (in background goroutine — blocks until observe cycle completes)
//  3. server dispatches device.observe over WebSocket
//  4. test goroutine reads device.observe, sends success response
//  5. orchestrator checkpoints → task reaches Terminal → POST goroutine unblocks
//  6. assert POST returned 201
func TestE2E_AgentHelloThenTaskDispatchesObserve(t *testing.T) {
	srv := buildTestServer(t)
	agentConn := dialWS(t, srv)

	// 1. agent.hello
	sendJSON(t, agentConn, map[string]any{
		"jsonrpc": "2.0",
		"id":      "hello-1",
		"method":  "agent.hello",
		"params": map[string]any{
			"deviceId":        "dev-e2e",
			"agentInstanceId": "inst-e2e",
			"capabilities":    []string{},
		},
	})

	helloResp := readJSON(t, agentConn)
	if helloResp["error"] != nil {
		t.Fatalf("agent.hello error: %v", helloResp["error"])
	}
	result, _ := helloResp["result"].(map[string]any)
	sessionID, _ := result["sessionId"].(string)
	if sessionID == "" {
		t.Fatalf("expected sessionId in hello response, got: %v", helloResp)
	}

	// 2. POST /tasks in a goroutine (blocks until observe cycle completes).
	type postResult struct {
		code int
		body []byte
	}
	postDone := make(chan postResult, 1)
	go func() {
		body := `{"goal":"e2e test goal","deviceId":"dev-e2e"}`
		resp, err := http.Post(srv.URL+"/tasks", "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Errorf("POST /tasks error: %v", err)
			postDone <- postResult{code: -1}
			return
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(resp.Body)
		postDone <- postResult{code: resp.StatusCode, body: buf.Bytes()}
	}()

	// 3. Read device.observe from WebSocket.
	observeMsg := readJSON(t, agentConn)
	method, _ := observeMsg["method"].(string)
	if method != "device.observe" {
		t.Fatalf("expected device.observe, got method=%q  full=%v", method, observeMsg)
	}
	cmdID, _ := observeMsg["id"].(string)
	if cmdID == "" {
		t.Fatal("device.observe missing id")
	}

	// 4. Agent responds with a success result.
	sendJSON(t, agentConn, map[string]any{
		"jsonrpc": "2.0",
		"id":      cmdID,
		"result": map[string]any{
			"snapshotId": "snap-001",
			"nodes":      []any{},
		},
	})

	// 5. Wait for POST to complete (orchestrator must have checkpointed).
	select {
	case pr := <-postDone:
		if pr.code != http.StatusCreated {
			t.Fatalf("POST /tasks expected 201, got %d: %s", pr.code, pr.body)
		}
		var created map[string]any
		_ = json.Unmarshal(pr.body, &created)
		if created["id"] == nil {
			t.Errorf("response missing task id: %s", pr.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for POST /tasks to complete")
	}
}

// TestE2E_Healthz verifies the healthz endpoint is reachable.
func TestE2E_Healthz(t *testing.T) {
	srv := buildTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestE2E_UnknownMethod_ErrorResponse verifies the server sends an error for unknown methods.
func TestE2E_UnknownMethod_ErrorResponse(t *testing.T) {
	srv := buildTestServer(t)
	conn := dialWS(t, srv)

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      "req-1",
		"method":  "agent.unknown",
		"params":  map[string]any{},
	})

	msg := readJSON(t, conn)
	if msg["error"] == nil {
		t.Errorf("expected error response for unknown method, got: %v", msg)
	}
}

type recordingProcessorWithLock struct {
	mu     sync.Mutex
	events []domain.Event
}

func (r *recordingProcessorWithLock) ProcessEvent(_ context.Context, e domain.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordingProcessorWithLock) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func TestE2E_AndroidEventIgnoredUntilRegistration(t *testing.T) {
	proc := &recordingProcessorWithLock{}
	srv := buildTestServerWithProcessor(t, proc)
	conn := dialWS(t, srv)

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"method":  "android.accessibility.disabled",
		"params": map[string]any{
			"seqNo": 1,
		},
	})
	time.Sleep(150 * time.Millisecond)
	if got := proc.count(); got != 0 {
		t.Fatalf("expected no pre-registration events, got %d", got)
	}

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      "hello-reg",
		"method":  "agent.hello",
		"params": map[string]any{
			"deviceId":        "dev-reg",
			"agentInstanceId": "inst-reg",
			"capabilities":    []string{},
		},
	})
	helloResp := readJSON(t, conn)
	if helloResp["error"] != nil {
		t.Fatalf("agent.hello error: %v", helloResp["error"])
	}

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"method":  "android.accessibility.disabled",
		"params": map[string]any{
			"seqNo": 2,
		},
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if proc.count() == 1 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("expected one post-registration android event, got %d", proc.count())
}
