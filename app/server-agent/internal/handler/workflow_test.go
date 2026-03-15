package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/handler"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

func newWorkflowHandler() (http.Handler, *workflow.MemoryDefStore) {
	defs := workflow.NewMemoryDefStore()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return handler.NewWorkflowHandler(defs, log), defs
}

func TestWorkflowHandler_PutRejectsInvalidWorkflow(t *testing.T) {
	h, defs := newWorkflowHandler()

	body := `
name: bad
entry: start
steps:
  start:
    trigger:
      kind: android.window.state_changed
    on_success: terminal
    on_failure: terminal
`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/workflows/bad", bytes.NewBufferString(body))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), string(domain.EventKindScreenChanged)) {
		t.Fatalf("expected replacement hint in response, got: %s", rec.Body.String())
	}
	if _, err := defs.Get(context.Background(), "bad"); err == nil {
		t.Fatal("invalid workflow should not be stored")
	}
}

func TestWorkflowHandler_PutStoresSemanticWorkflow(t *testing.T) {
	h, defs := newWorkflowHandler()

	body := `
name: login
entry: start
steps:
  start:
    trigger:
      kind: android.screen.changed
      ui:
        active_ui_key: login.ready
        ui_ready: true
        button_key: login.submit
        button_enabled: true
    action:
      kind: click
      target:
        kind: semantic_key
        value: login.submit
    expect:
      kind: android.screen.changed
      ui:
        active_ui_key: home.ready
        ui_ready: true
    on_success: terminal
    on_failure: terminal
`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/workflows/login", bytes.NewBufferString(body))
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	stored, err := defs.Get(context.Background(), "login")
	if err != nil {
		t.Fatalf("expected stored workflow, got error: %v", err)
	}
	if stored.Steps["start"].Action.Target.Kind != domain.TargetKindSemanticKey {
		t.Fatalf("target kind = %q, want %q", stored.Steps["start"].Action.Target.Kind, domain.TargetKindSemanticKey)
	}

	var got domain.WorkflowDef
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Steps["start"].Trigger.UI == nil || got.Steps["start"].Trigger.UI.ActiveUIKey != "login.ready" {
		t.Fatalf("expected semantic ui trigger in response, got: %#v", got.Steps["start"].Trigger.UI)
	}
}

