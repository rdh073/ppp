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
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

// noopProcessor satisfies usecase.EventProcessor — does nothing.
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
	uc := usecase.NewTaskControl(tasks, states, noopProcessor{}, reg, log)
	return handler.NewTaskHandler(uc, log), tasks, reg
}

func TestTaskHandler_CreateAndGet(t *testing.T) {
	h, tasks, _ := newTestHandler(t)

	// POST /tasks
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"goal":"automate something"}`))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /tasks: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&created)
	taskID, _ := created["id"].(string)
	if taskID == "" {
		t.Fatal("no id in response")
	}

	// Verify persisted.
	stored, err := tasks.Get(context.Background(), domain.TaskID(taskID))
	if err != nil {
		t.Fatalf("task not in store: %v", err)
	}
	if stored.Goal != "automate something" {
		t.Errorf("goal mismatch: %q", stored.Goal)
	}

	// GET /tasks/{id}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/"+taskID, nil)
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET /tasks/{id}: expected 200, got %d", rec2.Code)
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
