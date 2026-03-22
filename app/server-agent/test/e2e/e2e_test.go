//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/autosdk/ppp/server-agent/internal/devicectrl"
	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/eventing"
	"github.com/autosdk/ppp/server-agent/internal/eventruntime"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
	"github.com/autosdk/ppp/server-agent/internal/transport/ws"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflowruntime"
)

// buildTestServer spins up a complete server-agent HTTP server using
// in-memory stores. Returns a running httptest.Server; call ts.Close() to stop it.
// The workflowDef is seeded into the MemoryDefStore before returning.
func buildTestServer(t *testing.T, workflowDef *domain.WorkflowDef) *httptest.Server {
	t.Helper()

	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// --- in-memory stores ---
	taskStore := store.NewMemoryTaskStore()
	stateStore := store.NewMemoryWorkflowStateStore()
	eventStore := store.NewMemoryEventPlaneStore()
	commandOutbox := store.NewMemoryCommandOutboxStore()
	taskQueue := store.NewMemoryTaskQueue()

	// --- registry & dispatcher ---
	reg := registry.New()
	metricsRegistry := telemetry.NewRegistry()
	disp := dispatcher.NewMemoryDispatcher(reg, commandOutbox, metricsRegistry)

	// --- workflow def store ---
	defStore := workflow.NewMemoryDefStore()
	if workflowDef != nil {
		if err := defStore.Put(context.Background(), workflowDef.Name, workflowDef); err != nil {
			t.Fatalf("seed workflow def: %v", err)
		}
	}

	// --- workflow engine (no tool catalog needed for E2E scenarios) ---
	engine := workflow.NewEngine(defStore, disp)

	// --- orchestrator ---
	orch := orchestrator.New(taskStore, stateStore, engine, log, eventStore)

	// --- deadline watchdog ---
	serverCtx, serverCancel := context.WithCancel(context.Background())
	watchdog := orchestrator.NewDeadlineWatchdog(orch.ProcessAcceptedEvent, 100*time.Millisecond)
	orch.SetDeadlineWatchdog(watchdog)
	go watchdog.Run(serverCtx)

	// --- event runtime (inline) ---
	runtime := eventruntime.NewInlineRuntime(eventStore, orch, log)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("start event runtime: %v", err)
	}

	// --- use cases ---
	recoveryUC := workflowruntime.NewRuntimeRecovery(taskStore, stateStore, log)
	recoveryUC.SetQueue(taskQueue)
	if _, err := recoveryUC.Recover(context.Background()); err != nil {
		t.Fatalf("runtime recovery: %v", err)
	}

	assigner := workflowruntime.NewDeviceAssigner(taskStore, taskQueue, reg, runtime, log)
	orch.SetOnTaskTerminal(assigner.OnTaskTerminal)

	lifecycleUC := devicectrl.NewAgentLifecycle(reg, runtime, nil, nil, log)
	lifecycleUC.SetAssigner(assigner)

	taskUC := workflowruntime.NewTaskControl(taskStore, stateStore, runtime, reg, log)
	taskUC.SetAssigner(assigner)

	// noop auto-enabler — no ADB in tests
	eventUC := eventing.NewEventIngestion(runtime)
	eventPlaneUC := eventing.NewEventPlaneControl(eventStore, runtime, eventUC, log)

	// --- handlers ---
	agentHandler := handler.NewAgentHandler(lifecycleUC, log)
	taskHandler := handler.NewTaskHandler(taskUC, log)
	workflowHandler := handler.NewWorkflowHandler(defStore, log)
	eventPlaneHandler := handler.NewEventPlaneHandler(eventPlaneUC, log)
	agentServer := ws.NewAgentServer(agentHandler, eventUC, reg, disp, log)

	// --- HTTP mux ---
	mux := http.NewServeMux()
	mux.Handle("/ws/agent", agentServer)
	mux.Handle("/tasks", taskHandler)
	mux.Handle("/tasks/", taskHandler)
	mux.Handle("/workflows", workflowHandler)
	mux.Handle("/workflows/", workflowHandler)
	mux.Handle("/events", eventPlaneHandler)
	mux.Handle("/events/", eventPlaneHandler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	ts := httptest.NewServer(mux)

	// Cancel the watchdog goroutine when the test server is closed.
	t.Cleanup(func() {
		serverCancel()
		ts.Close()
	})

	return ts
}

// --- mock agent ---

// mockAgent simulates an Android agent over WebSocket/JSON-RPC.
type mockAgent struct {
	mu        sync.Mutex
	conn      *websocket.Conn
	deviceID  domain.DeviceID
	sessionID string
	t         *testing.T

	// handlers called when the server issues device commands
	onExecute func(params json.RawMessage) (json.RawMessage, error)
	onObserve func() (json.RawMessage, error)
}

func newMockAgent(t *testing.T, deviceID domain.DeviceID) *mockAgent {
	t.Helper()
	return &mockAgent{
		t:        t,
		deviceID: deviceID,
	}
}

func (a *mockAgent) connect(serverURL string) error {
	// Convert http:// → ws://
	wsURL := strings.Replace(serverURL, "http://", "ws://", 1) + "/ws/agent"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", wsURL, err)
	}
	a.conn = conn
	return nil
}

func (a *mockAgent) close() {
	if a.conn != nil {
		_ = a.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = a.conn.Close()
	}
}

type rpcMsg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (a *mockAgent) send(msg any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conn.WriteJSON(msg)
}

func (a *mockAgent) sendRequest(id, method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return a.send(rpcMsg{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  raw,
	})
}

func (a *mockAgent) sendResult(id string, result json.RawMessage) error {
	return a.send(rpcMsg{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (a *mockAgent) sendNotification(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return a.send(rpcMsg{
		JSONRPC: "2.0",
		Method:  method,
		Params:  raw,
	})
}

// hello performs the agent.hello handshake and stores the returned sessionID.
func (a *mockAgent) hello() error {
	if err := a.sendRequest("hello-1", "agent.hello", map[string]any{
		"deviceId":        string(a.deviceID),
		"agentInstanceId": "mock-agent-1",
		"capabilities":    []string{},
	}); err != nil {
		return fmt.Errorf("send agent.hello: %w", err)
	}

	// Read the response.
	_, raw, err := a.conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read agent.hello response: %w", err)
	}
	var resp rpcMsg
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("decode agent.hello response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("agent.hello error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	var result struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("decode agent.hello result: %w", err)
	}
	a.sessionID = result.SessionID
	return nil
}

// disconnect sends agent.disconnect and closes the connection.
func (a *mockAgent) disconnect() error {
	if err := a.sendRequest("disc-1", "agent.disconnect", map[string]any{
		"deviceId":  string(a.deviceID),
		"sessionId": a.sessionID,
	}); err != nil {
		return fmt.Errorf("send agent.disconnect: %w", err)
	}
	// Read ack (may fail if server already closed — that's fine).
	a.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, _ = a.conn.ReadMessage()
	a.conn.SetReadDeadline(time.Time{})
	return nil
}

// readLoop reads inbound server requests in a goroutine until ctx is cancelled
// or the connection closes. Handles device.execute and device.observe calls.
func (a *mockAgent) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Set a short read deadline so we can check ctx.Done() periodically.
		a.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		_, raw, err := a.conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
				return
			}
			// Only continue the loop on read-deadline timeouts. Any other error
			// (including gorilla's "use of closed network connection") means the
			// connection is broken and gorilla will panic on a subsequent read.
			type netErr interface{ Timeout() bool }
			if ne, ok := err.(netErr); ok && ne.Timeout() {
				continue
			}
			return
		}

		var msg rpcMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			a.t.Logf("mockAgent: decode error: %v", err)
			continue
		}

		// Only handle server-to-agent requests (method is set, id is set).
		if msg.Method == "" || msg.ID == "" {
			continue
		}

		switch msg.Method {
		case "device.execute":
			var result json.RawMessage
			var execErr error
			if a.onExecute != nil {
				result, execErr = a.onExecute(msg.Params)
			} else {
				result = json.RawMessage(`{}`)
			}
			var sendErr error
			if execErr != nil {
				sendErr = a.send(rpcMsg{
					JSONRPC: "2.0",
					ID:      msg.ID,
					Error:   &rpcError{Code: -32603, Message: execErr.Error()},
				})
			} else {
				sendErr = a.sendResult(msg.ID, result)
			}
			// A write failure means gorilla has closed c.done; reading again panics.
			if sendErr != nil {
				return
			}

		case "device.observe":
			var result json.RawMessage
			if a.onObserve != nil {
				result, _ = a.onObserve()
			} else {
				result = json.RawMessage(`{"packageName":"","activityName":"","targets":[]}`)
			}
			if err := a.sendResult(msg.ID, result); err != nil {
				return
			}

		default:
			a.t.Logf("mockAgent: unknown server method %q", msg.Method)
		}
	}
}

// --- HTTP helpers ---

type taskResponse struct {
	ID             string            `json:"id"`
	Goal           string            `json:"goal"`
	Status         string            `json:"status"`
	AssignedDevice string            `json:"assignedDevice"`
	InputArtifacts map[string]string `json:"inputArtifacts"`
}

func createTask(t *testing.T, serverURL string, goal string, workflowName string) taskResponse {
	t.Helper()
	body := fmt.Sprintf(`{"goal":%q,"workflowName":%q}`, goal, workflowName)
	resp, err := http.Post(serverURL+"/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /tasks: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST /tasks: status %d: %s", resp.StatusCode, b)
	}
	var task taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	return task
}

func getTask(t *testing.T, serverURL string, taskID string) taskResponse {
	t.Helper()
	resp, err := http.Get(serverURL + "/tasks/" + taskID)
	if err != nil {
		t.Fatalf("GET /tasks/%s: %v", taskID, err)
	}
	defer resp.Body.Close()
	var task taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	return task
}

func pollTaskUntil(
	t *testing.T,
	serverURL string,
	taskID string,
	condition func(taskResponse) bool,
	timeout time.Duration,
) taskResponse {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		task := getTask(t, serverURL, taskID)
		if condition(task) {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("pollTaskUntil: timed out after %s; last state: %+v", timeout, task)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// --- tests ---

// TestE2E_SimpleWorkflow_Completes verifies the full stack end-to-end:
// WebSocket → JSON-RPC → event ingestion → orchestrator → workflow engine → terminal state.
//
// Workflow: single action step that clicks a button. The mock agent returns a
// snapshotAfter whose packageName matches the Expect condition, so the engine
// advances immediately to "terminal" without waiting for a second event.
func TestE2E_SimpleWorkflow_Completes(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "e2e-simple",
		Entry: "click_login",
		Steps: map[string]domain.StepDef{
			"click_login": {
				Trigger: domain.EventMatch{},
				Action: &domain.ActionDef{
					Kind:   domain.ActionKindClick,
					Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "Login"},
				},
				Expect: &domain.ExpectDef{
					Package: "com.example",
				},
				Timeout:   "5s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}

	ts := buildTestServer(t, def)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Connect mock agent.
	agent := newMockAgent(t, "device-e2e-001")

	// The mock agent returns a snapshotAfter that matches the Expect (package = "com.example").
	agent.onExecute = func(_ json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{
			"snapshotAfter": {
				"packageName": "com.example",
				"activityName": "com.example.MainActivity",
				"targets": []
			}
		}`), nil
	}

	if err := agent.connect(ts.URL); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer agent.close()

	if err := agent.hello(); err != nil {
		t.Fatalf("hello: %v", err)
	}

	// Start read loop so the agent can respond to device.execute calls.
	go agent.readLoop(ctx)

	// Create task — no explicit deviceId so assigner routes to the idle device.
	task := createTask(t, ts.URL, "click login button", "e2e-simple")
	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	t.Logf("task created: %s", task.ID)

	// Poll until completed or timeout.
	final := pollTaskUntil(t, ts.URL, task.ID, func(tr taskResponse) bool {
		return tr.Status == "completed" || tr.Status == "failed" || tr.Status == "cancelled"
	}, 10*time.Second)

	if final.Status != "completed" {
		t.Errorf("expected task status=completed, got %q", final.Status)
	}
}

// TestE2E_AccessibilityDisabled_TriggersWorkflow verifies that sending an
// android.accessibility.disabled notification via the WebSocket triggers
// the workflow step keyed on that event kind and advances to terminal.
func TestE2E_AccessibilityDisabled_TriggersWorkflow(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "e2e-accessibility",
		Entry: "wait_accessible",
		Steps: map[string]domain.StepDef{
			"wait_accessible": {
				// Trigger matches the android.accessibility.disabled event.
				Trigger:   domain.EventMatch{Kind: domain.EventKindAccessibilityDisabled},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}

	ts := buildTestServer(t, def)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agent := newMockAgent(t, "device-e2e-002")
	if err := agent.connect(ts.URL); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer agent.close()

	if err := agent.hello(); err != nil {
		t.Fatalf("hello: %v", err)
	}

	go agent.readLoop(ctx)

	// Create the task first so it gets assigned to this device.
	task := createTask(t, ts.URL, "enable accessibility service", "e2e-accessibility")
	t.Logf("task created: %s", task.ID)

	// Wait until the task is running (assigned to the device) before sending the event.
	pollTaskUntil(t, ts.URL, task.ID, func(tr taskResponse) bool {
		return tr.Status == "running"
	}, 5*time.Second)

	// Send the triggering notification with seqNo=1.
	if err := agent.sendNotification("android.accessibility.disabled", map[string]any{
		"seqNo": 1,
	}); err != nil {
		t.Fatalf("sendNotification: %v", err)
	}

	// Poll until task completes.
	final := pollTaskUntil(t, ts.URL, task.ID, func(tr taskResponse) bool {
		return tr.Status == "completed" || tr.Status == "failed" || tr.Status == "cancelled"
	}, 10*time.Second)

	if final.Status != "completed" {
		t.Errorf("expected task status=completed, got %q", final.Status)
	}
}

// TestE2E_DeviceOffline_RequeuesTask verifies that when a device disconnects
// mid-task, the task is re-queued and then assigned to the next connecting device.
func TestE2E_DeviceOffline_RequeuesTask(t *testing.T) {
	// Workflow with a specific trigger so the engine stays waiting and doesn't
	// auto-complete before we can disconnect the first agent.
	def := &domain.WorkflowDef{
		Name:  "e2e-requeue",
		Entry: "wait_event",
		Steps: map[string]domain.StepDef{
			"wait_event": {
				// Trigger on a specific event that we will never send, so the task stays running.
				Trigger:   domain.EventMatch{Kind: domain.EventKindActivityCreated},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}

	ts := buildTestServer(t, def)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- Agent A connects and takes the task ---
	agentA := newMockAgent(t, "device-e2e-a")
	if err := agentA.connect(ts.URL); err != nil {
		t.Fatalf("agentA connect: %v", err)
	}
	if err := agentA.hello(); err != nil {
		t.Fatalf("agentA hello: %v", err)
	}

	loopCtxA, cancelA := context.WithCancel(ctx)
	go agentA.readLoop(loopCtxA)
	defer cancelA()

	// Create task — should be assigned to agentA since it is the only device.
	task := createTask(t, ts.URL, "wait for activity", "e2e-requeue")
	t.Logf("task created: %s", task.ID)

	// Wait until assigned to agentA.
	pollTaskUntil(t, ts.URL, task.ID, func(tr taskResponse) bool {
		return tr.AssignedDevice == "device-e2e-a" && tr.Status == "running"
	}, 5*time.Second)
	t.Log("task assigned to agentA")

	// --- Disconnect Agent A ---
	cancelA() // stop read loop first
	if err := agentA.disconnect(); err != nil {
		t.Logf("agentA disconnect (best effort): %v", err)
	}
	agentA.close()

	// Task should be paused and re-queued.
	pollTaskUntil(t, ts.URL, task.ID, func(tr taskResponse) bool {
		return tr.Status == "paused" || tr.AssignedDevice == ""
	}, 5*time.Second)
	t.Log("task paused after agentA disconnect")

	// --- Agent B connects and should pick up the task ---
	agentB := newMockAgent(t, "device-e2e-b")
	if err := agentB.connect(ts.URL); err != nil {
		t.Fatalf("agentB connect: %v", err)
	}
	defer agentB.close()

	if err := agentB.hello(); err != nil {
		t.Fatalf("agentB hello: %v", err)
	}

	go agentB.readLoop(ctx)

	// Task should be re-assigned to agentB.
	final := pollTaskUntil(t, ts.URL, task.ID, func(tr taskResponse) bool {
		return tr.AssignedDevice == "device-e2e-b" && tr.Status == "running"
	}, 10*time.Second)

	if final.AssignedDevice != "device-e2e-b" {
		t.Errorf("expected task assigned to device-e2e-b, got %q", final.AssignedDevice)
	}
	if final.Status != "running" {
		t.Errorf("expected task status=running after reassignment, got %q", final.Status)
	}
}

// TestE2E_DeadlineWatchdog_RetriesAndCompletes validates the full "UI no
// transition" recovery path:
//
//  1. Action dispatched — mock returns snapshotAfter that does NOT match expect.
//  2. WaitingExpect is armed; watchdog ticks after 200 ms timeout.
//  3. handleFailure increments RetryCount; next execute call is the retry.
//  4. Retry mock returns a matching snapshotAfter — engine advances immediately.
//  5. Task completes as "completed".
func TestE2E_DeadlineWatchdog_RetriesAndCompletes(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "watchdog-retry",
		Entry: "click",
		Steps: map[string]domain.StepDef{
			"click": {
				Trigger: domain.EventMatch{},
				Action:  &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
				Expect: &domain.ExpectDef{
					Package: "com.example.success",
				},
				Timeout:   "150ms", // short so watchdog (100ms tick) fires within ~250ms
				MaxRetry:  1,
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	ts := buildTestServer(t, def)

	var mu sync.Mutex
	attempt := 0

	agent := newMockAgent(t, "device-watchdog")
	agent.onExecute = func(_ json.RawMessage) (json.RawMessage, error) {
		mu.Lock()
		attempt++
		n := attempt
		mu.Unlock()
		if n == 1 {
			// First attempt: snapshot does NOT match → WaitingExpect is armed.
			// Watchdog will fire after 150ms + up to 100ms tick = ≤ 250ms.
			return json.RawMessage(`{"snapshotAfter":{"packageName":"com.other","activityName":"Other","targets":[]}}`), nil
		}
		// Retry attempt: snapshot matches → immediate advance to terminal.
		return json.RawMessage(`{"snapshotAfter":{"packageName":"com.example.success","activityName":"SuccessActivity","targets":[]}}`), nil
	}

	if err := agent.connect(ts.URL); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := agent.hello(); err != nil {
		t.Fatalf("hello: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go agent.readLoop(ctx)

	task := createTask(t, ts.URL, "watchdog retry", "watchdog-retry")
	t.Logf("task created: %s", task.ID)

	final := pollTaskUntil(t, ts.URL, task.ID, func(tr taskResponse) bool {
		return tr.Status == "completed" || tr.Status == "failed"
	}, 8*time.Second)

	mu.Lock()
	totalAttempts := attempt
	mu.Unlock()

	if final.Status != "completed" {
		t.Errorf("expected completed, got %q (execute attempts=%d)", final.Status, totalAttempts)
	}
	if totalAttempts < 2 {
		t.Errorf("expected at least 2 execute attempts (initial + 1 retry), got %d", totalAttempts)
	}
	t.Logf("completed after %d execute attempt(s)", totalAttempts)
}
