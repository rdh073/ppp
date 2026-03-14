package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/exampleprovider"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

func defaultToolDir() string {
	return filepath.Join("..", "..", "config", "tools")
}

func exampleHTTPToolDir() string {
	return filepath.Join("..", "..", "config", "examples", "http-provider")
}

func TestLoadCatalog_DefaultConfig_ExposesExpectedToolsAndBindings(t *testing.T) {
	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	for _, toolName := range []string{
		"identity.generate_indonesian_name",
		"identity.generate_email",
		"identity.generate_alias_email",
		"credential.generate_password",
		"identity.generate_birth_date",
		"content.generate_welcome_email",
	} {
		if _, ok := catalog.Registry.Manifest(toolName); !ok {
			t.Fatalf("expected manifest for %s", toolName)
		}
	}

	for _, bindingID := range []string{
		"example_remote.generate_alias_email",
		"local_identity.generate_name",
		"local_identity.generate_email",
		"local_identity.generate_password",
		"local_identity.generate_birth_date",
		"local_identity.generate_welcome_email",
	} {
		if _, ok := catalog.Bindings.Binding(bindingID); !ok {
			t.Fatalf("expected binding %s", bindingID)
		}
	}
}

func TestLoadCatalog_DefaultConfig_LocalToolInvokesFromManifest(t *testing.T) {
	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	raw, err := catalog.Registry.Invoke(context.Background(), "identity.generate_email", json.RawMessage(`{"fullName":"Ayu Lestari","domain":"example.id"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var result struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Email != "ayu.lestari@example.id" {
		t.Fatalf("unexpected email %q", result.Email)
	}
}

func TestLoadCatalog_DefaultConfig_ModelToolRemainsVisibleWhenDisabled(t *testing.T) {
	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if _, ok := catalog.Registry.Manifest("content.generate_welcome_email"); !ok {
		t.Fatal("expected content.generate_welcome_email manifest")
	}
	_, err = catalog.Registry.Invoke(context.Background(), "content.generate_welcome_email", json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if !errors.Is(err, nodes.ErrToolDisabled) {
		t.Fatalf("expected ErrToolDisabled, got %v", err)
	}
}

func TestLoadCatalog_DefaultConfig_HTTPToolRemainsVisibleWhenProviderDisabled(t *testing.T) {
	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if _, ok := catalog.Registry.Manifest(exampleprovider.ToolName); !ok {
		t.Fatalf("expected %s manifest", exampleprovider.ToolName)
	}
	if _, ok := catalog.Bindings.Binding("example_remote.generate_alias_email"); !ok {
		t.Fatal("expected example_remote.generate_alias_email binding")
	}
	_, err = catalog.Registry.Invoke(context.Background(), exampleprovider.ToolName, json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if !errors.Is(err, nodes.ErrToolDisabled) {
		t.Fatalf("expected ErrToolDisabled, got %v", err)
	}
}

func TestLoadCatalog_DefaultConfig_HTTPProviderInvokesWhenConfigured(t *testing.T) {
	server := httptest.NewServer(exampleprovider.NewHandler(nil))
	defer server.Close()

	t.Setenv("AUTO_TOOL_EXAMPLE_BASE_URL", server.URL)

	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	assertAliasEmailBindingRun(t, catalog, "profile_alias_email", "profile_alias_email_source")
}

func TestLoadCatalog_HTTPProvider_EndToEndViaBinding(t *testing.T) {
	server := httptest.NewServer(exampleprovider.NewHandler(nil))
	defer server.Close()

	t.Setenv("AUTO_TOOL_EXAMPLE_BASE_URL", server.URL)

	catalog, err := tools.LoadCatalog(context.Background(), exampleHTTPToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	assertAliasEmailBindingRun(t, catalog, "profile_alias_email", "profile_alias_email_source")
}

func assertAliasEmailBindingRun(t *testing.T, catalog *tools.LoadedCatalog, emailArtifact string, sourceArtifact string) {
	t.Helper()

	node := nodes.NewToolCallNodeWithBindings(catalog.Registry, catalog.Bindings)
	state := domain.NewWorkflowState(domain.TaskID("task-http"), domain.DeviceID("device-http"))
	state.Artifacts["profile_full_name"] = "Ayu Lestari"
	state.Artifacts["pending_tool_binding"] = "example_remote.generate_alias_email"
	task := &domain.Task{
		ID:        domain.TaskID("task-http"),
		Goal:      "generate alias email",
		Status:    domain.TaskStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	out, err := node.Run(context.Background(), workflow.NodeInput{State: state, Task: task})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Fatalf("expected success, got %s", out.Status)
	}
	if out.Artifacts[emailArtifact] != "ayu.lestari.sandbox@remote.autosdk.id" {
		t.Fatalf("unexpected alias email %q", out.Artifacts[emailArtifact])
	}
	if out.Artifacts[sourceArtifact] != "example-http-provider" {
		t.Fatalf("unexpected alias email source %q", out.Artifacts[sourceArtifact])
	}
	if out.Artifacts["last_tool_name"] != exampleprovider.ToolName {
		t.Fatalf("unexpected last_tool_name %q", out.Artifacts["last_tool_name"])
	}
	if out.Artifacts["last_tool_binding"] != "example_remote.generate_alias_email" {
		t.Fatalf("unexpected last_tool_binding %q", out.Artifacts["last_tool_binding"])
	}
	if len(out.EmittedEvents) != 1 {
		t.Fatalf("expected one emitted event, got %d", len(out.EmittedEvents))
	}
	payload, ok := out.EmittedEvents[0].Payload.(domain.ToolResultPayload)
	if !ok {
		t.Fatalf("expected ToolResultPayload, got %T", out.EmittedEvents[0].Payload)
	}
	if payload.BindingID != "example_remote.generate_alias_email" {
		t.Fatalf("unexpected binding id %q", payload.BindingID)
	}
	if payload.ToolName != exampleprovider.ToolName {
		t.Fatalf("unexpected tool name %q", payload.ToolName)
	}
	if payload.ErrString != "" {
		t.Fatalf("expected empty tool error, got %q", payload.ErrString)
	}
}
