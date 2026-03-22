package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/workflowruntime"
)

// noopProcessor satisfies workflowruntime.EventProcessor — does nothing.
type noopProcessor struct{}

func (noopProcessor) ProcessEvent(_ context.Context, _ domain.Event) error { return nil }

// noopSender satisfies registry.Sender.
type noopSender struct{}

func (noopSender) SendRequest(_, _ string, _ any) error      { return nil }
func (noopSender) SendSuccess(_ string, _ any) error         { return nil }
func (noopSender) SendError(_ string, _ int, _ string) error { return nil }
func (noopSender) Close() error                              { return nil }

func newTestHandler(t *testing.T) (http.Handler, *store.MemoryTaskStore, *registry.Registry) {
	t.Helper()
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	reg := registry.New()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	uc := workflowruntime.NewTaskControl(tasks, states, noopProcessor{}, reg, log)
	return handler.NewTaskHandler(uc, log), tasks, reg
}

func TestTaskHandler_CreateAndGet(t *testing.T) {
	h, tasks, _ := newTestHandler(t)

	// POST /tasks
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":"automate something","inputArtifacts":{"account.email":"ada@example.com","account.name":"Ada"}}`))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tasks: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID             string            `json:"id"`
		InputArtifacts map[string]string `json:"inputArtifacts"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if created.ID == "" {
		t.Fatal("no id in response")
	}
	if created.InputArtifacts["account.email"] != "ada@example.com" || created.InputArtifacts["account.name"] != "Ada" {
		t.Fatalf("unexpected inputArtifacts in create response: %#v", created.InputArtifacts)
	}

	// Verify persisted.
	stored, err := tasks.Get(context.Background(), domain.TaskID(created.ID))
	if err != nil {
		t.Fatalf("task not in store: %v", err)
	}
	if stored.Goal != "automate something" {
		t.Errorf("goal mismatch: %q", stored.Goal)
	}
	if stored.InputArtifacts["account.email"] != "ada@example.com" || stored.InputArtifacts["account.name"] != "Ada" {
		t.Fatalf("unexpected persisted inputArtifacts: %#v", stored.InputArtifacts)
	}

	// GET /tasks/{id}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/"+created.ID, nil)
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /tasks/{id}: expected 200, got %d", rec2.Code)
	}
	var got struct {
		InputArtifacts map[string]string `json:"inputArtifacts"`
	}
	_ = json.NewDecoder(rec2.Body).Decode(&got)
	if got.InputArtifacts["account.email"] != "ada@example.com" || got.InputArtifacts["account.name"] != "Ada" {
		t.Fatalf("unexpected inputArtifacts in get response: %#v", got.InputArtifacts)
	}
}

func TestTaskHandler_EmptyGoal_400(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":""}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestTaskHandler_GetNotFound_404(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks/nonexistent", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestTaskHandler_Cancel(t *testing.T) {
	h, _, _ := newTestHandler(t)

	// Create a task.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":"cancel me"}`)))
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	taskID := created["id"].(string)

	// DELETE it.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest(http.MethodDelete, "/tasks/"+taskID, nil))
	if rec2.Code != http.StatusNoContent {
		t.Errorf("DELETE /tasks/{id}: expected 204, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestTaskHandler_CreateWithConnectedDevice_Running(t *testing.T) {
	h, _, reg := newTestHandler(t)

	// Pre-register a device.
	sess := &domain.Session{
		ID:              domain.NewSessionID(),
		DeviceID:        "dev-http-test",
		ConnectedAt:     time.Now(),
		LastHeartbeatAt: time.Now(),
	}
	_ = reg.Add(sess, noopSender{})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/tasks",
		bytes.NewBufferString(`{"goal":"run on device","deviceId":"dev-http-test"}`)))

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	if created["status"] != "running" {
		t.Errorf("expected status=running, got %v", created["status"])
	}
}

func TestTaskHandler_List(t *testing.T) {
	h, _, _ := newTestHandler(t)

	recCreate1 := httptest.NewRecorder()
	h.ServeHTTP(recCreate1, httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":"task-one","workflowName":"wf-a"}`)))
	if recCreate1.Code != http.StatusCreated {
		t.Fatalf("create task one: %d %s", recCreate1.Code, recCreate1.Body.String())
	}

	recCreate2 := httptest.NewRecorder()
	h.ServeHTTP(recCreate2, httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":"task-two","workflowName":"wf-b"}`)))
	if recCreate2.Code != http.StatusCreated {
		t.Fatalf("create task two: %d %s", recCreate2.Code, recCreate2.Body.String())
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks?limit=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("expected 2 tasks in list, got %d", len(payload))
	}
	if payload[0]["workflowName"] == nil {
		t.Fatalf("expected workflowName in task payload: %#v", payload[0])
	}
}

func TestTaskHandler_ListWithOffset(t *testing.T) {
	h, _, _ := newTestHandler(t)

	recCreate1 := httptest.NewRecorder()
	h.ServeHTTP(recCreate1, httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":"task-one","workflowName":"wf-a"}`)))
	if recCreate1.Code != http.StatusCreated {
		t.Fatalf("create task one: %d %s", recCreate1.Code, recCreate1.Body.String())
	}

	recCreate2 := httptest.NewRecorder()
	h.ServeHTTP(recCreate2, httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":"task-two","workflowName":"wf-b"}`)))
	if recCreate2.Code != http.StatusCreated {
		t.Fatalf("create task two: %d %s", recCreate2.Code, recCreate2.Body.String())
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tasks?limit=1&offset=1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tasks: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 task in list, got %d", len(payload))
	}
}
