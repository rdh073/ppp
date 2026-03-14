package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

func defaultToolDir() string {
	return filepath.Join("..", "..", "config", "tools")
}

func TestLoadCatalog_DefaultConfig_ExposesExpectedToolsAndBindings(t *testing.T) {
	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	for _, toolName := range []string{
		"identity.generate_indonesian_name",
		"identity.generate_email",
		"credential.generate_password",
		"identity.generate_birth_date",
		"content.generate_welcome_email",
	} {
		if _, ok := catalog.Registry.Manifest(toolName); !ok {
			t.Fatalf("expected manifest for %s", toolName)
		}
	}

	for _, bindingID := range []string{
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
