package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/exampleprovider"
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
		"identity.generate_persona",
		"identity.generate_alias_email",
		"credential.generate_password",
		"identity.generate_birth_date",
		"content.generate_welcome_email",
	} {
		if _, ok := catalog.Registry.Manifest(toolName); !ok {
			t.Fatalf("expected manifest for %s", toolName)
		}
	}

	for _, toolName := range []string{
		"identity.generate_indonesian_name",
		"identity.generate_email",
	} {
		if _, ok := catalog.Registry.Manifest(toolName); ok {
			t.Fatalf("tool %s should have been removed (now LLM-backed as identity.generate_persona)", toolName)
		}
	}

	for _, bindingID := range []string{
		"example_remote.generate_alias_email",
		"local_identity.generate_persona",
		"local_identity.generate_password",
		"local_identity.generate_birth_date",
		"local_identity.generate_welcome_email",
	} {
		if _, ok := catalog.Bindings.Binding(bindingID); !ok {
			t.Fatalf("expected binding %s", bindingID)
		}
	}

	for _, bindingID := range []string{
		"local_identity.generate_name",
		"local_identity.generate_email",
	} {
		if _, ok := catalog.Bindings.Binding(bindingID); ok {
			t.Fatalf("binding %s should have been removed", bindingID)
		}
	}
}

func TestLoadCatalog_DefaultConfig_LocalToolInvokesFromManifest(t *testing.T) {
	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	// credential.generate_password is the canonical local deterministic tool.
	raw, err := catalog.Registry.Invoke(context.Background(), "credential.generate_password", nil)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var result struct {
		Password string `json:"password"`
		Length   int    `json:"length"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Password == "" || result.Length == 0 {
		t.Fatal("expected non-empty password from local tool")
	}
}

// TestLoadCatalog_WelcomeEmail_RemainsVisibleWhenAllDisabled verifies that the
// content.generate_welcome_email chain tool is loadable with no providers
// configured and returns ErrToolDisabled (not a load-time error).
func TestLoadCatalog_WelcomeEmail_RemainsVisibleWhenAllDisabled(t *testing.T) {
	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if _, ok := catalog.Registry.Manifest("content.generate_welcome_email"); !ok {
		t.Fatal("expected content.generate_welcome_email manifest")
	}
	_, err = catalog.Registry.Invoke(context.Background(), "content.generate_welcome_email", json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if !errors.Is(err, tools.ErrToolDisabled) {
		t.Fatalf("expected ErrToolDisabled when all providers unconfigured, got %v", err)
	}
}

// TestLoadCatalog_WelcomeEmail_InvokesViaOpenAI configures only the openai-native
// provider and verifies the chain selects it and returns a valid result.
func TestLoadCatalog_WelcomeEmail_InvokesViaOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer openai-key" {
			t.Errorf("unexpected Authorization %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "gpt-test" {
			t.Errorf("unexpected model %#v", body["model"])
		}
		if _, ok := body["response_format"]; !ok {
			t.Error("expected response_format in request")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"subject\":\"Selamat datang\",\"body\":\"Halo Ayu\",\"language\":\"id\",\"tone\":\"professional_warm\"}"}}]}`))
	}))
	defer srv.Close()

	t.Setenv("AUTO_TOOL_OPENAI_API_URL", srv.URL)
	t.Setenv("AUTO_TOOL_OPENAI_API_KEY", "openai-key")
	t.Setenv("AUTO_TOOL_OPENAI_MODEL", "gpt-test")

	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	assertWelcomeEmailInvokeTool(t, catalog, "content.generate_welcome_email")
}

// TestLoadCatalog_WelcomeEmail_InvokesViaDeepSeek configures only the deepseek-native
// provider; the chain skips openai (disabled) and reaches deepseek.
func TestLoadCatalog_WelcomeEmail_InvokesViaDeepSeek(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer deepseek-key" {
			t.Errorf("unexpected Authorization %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "deepseek-chat" {
			t.Errorf("unexpected model %#v", body["model"])
		}
		if body["tool_choice"] != "required" {
			t.Errorf("unexpected tool_choice %#v", body["tool_choice"])
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"tool_calls":[{"type":"function","function":{"name":"content_generate_welcome_email","arguments":"{\"subject\":\"Selamat datang\",\"body\":\"Halo Ayu\",\"language\":\"id\",\"tone\":\"professional_warm\"}"}}]}}]}`))
	}))
	defer srv.Close()

	t.Setenv("AUTO_TOOL_DEEPSEEK_API_URL", srv.URL)
	t.Setenv("AUTO_TOOL_DEEPSEEK_API_KEY", "deepseek-key")
	t.Setenv("AUTO_TOOL_DEEPSEEK_MODEL", "deepseek-chat")

	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	assertWelcomeEmailInvokeTool(t, catalog, "content.generate_welcome_email")
}

// TestLoadCatalog_WelcomeEmail_InvokesViaAnthropic configures only the anthropic-native
// provider; the chain skips openai and deepseek (disabled) and reaches anthropic.
func TestLoadCatalog_WelcomeEmail_InvokesViaAnthropic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "anthropic-key" {
			t.Errorf("unexpected x-api-key %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("unexpected anthropic-version %q", got)
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"tool_use","name":"content_generate_welcome_email","input":{"subject":"Selamat datang","body":"Halo Ayu","language":"id","tone":"professional_warm"}}]}`))
	}))
	defer srv.Close()

	t.Setenv("AUTO_TOOL_ANTHROPIC_API_URL", srv.URL)
	t.Setenv("AUTO_TOOL_ANTHROPIC_API_KEY", "anthropic-key")
	t.Setenv("AUTO_TOOL_ANTHROPIC_MODEL", "claude-test")

	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	assertWelcomeEmailInvokeTool(t, catalog, "content.generate_welcome_email")
}

// TestLoadCatalog_WelcomeEmail_InvokesViaGemini configures only the gemini-native
// provider; the chain skips openai, deepseek, and anthropic (disabled) and reaches gemini.
func TestLoadCatalog_WelcomeEmail_InvokesViaGemini(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/gemini-test:generateContent" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "gemini-key" {
			t.Errorf("unexpected x-goog-api-key %q", got)
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"{\"subject\":\"Selamat datang\",\"body\":\"Halo Ayu\",\"language\":\"id\",\"tone\":\"professional_warm\"}"}]}}]}`))
	}))
	defer srv.Close()

	t.Setenv("AUTO_TOOL_GEMINI_API_URL", srv.URL)
	t.Setenv("AUTO_TOOL_GEMINI_API_KEY", "gemini-key")
	t.Setenv("AUTO_TOOL_GEMINI_MODEL", "gemini-test")

	catalog, err := tools.LoadCatalog(context.Background(), defaultToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	assertWelcomeEmailInvokeTool(t, catalog, "content.generate_welcome_email")
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
	if !errors.Is(err, tools.ErrToolDisabled) {
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

	raw, err := catalog.Registry.Invoke(context.Background(), exampleprovider.ToolName, json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var result struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Email == "" {
		t.Fatal("expected non-empty alias email from HTTP provider")
	}
}

func TestLoadCatalog_HTTPProvider_EndToEndViaRegistry(t *testing.T) {
	server := httptest.NewServer(exampleprovider.NewHandler(nil))
	defer server.Close()

	t.Setenv("AUTO_TOOL_EXAMPLE_BASE_URL", server.URL)

	catalog, err := tools.LoadCatalog(context.Background(), exampleHTTPToolDir(), nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	raw, err := catalog.Registry.Invoke(context.Background(), exampleprovider.ToolName, json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	var result struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Email == "" {
		t.Fatal("expected non-empty alias email from HTTP example catalog")
	}
}


// TestLoadCatalog_ChainProvider_FallsBackWhenFirstDisabled verifies that when
// the first provider in a chain is disabled (optional, unconfigured), the chain
// advances and the second provider's result is returned.
func TestLoadCatalog_ChainProvider_FallsBackWhenFirstDisabled(t *testing.T) {
	// Second provider (chain-second) returns a canned result.
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"choices":[{"message":{"content":"{\"result\":\"ok-from-fallback\"}"}}]
		}`))
	}))
	defer fallback.Close()

	// Build a minimal temp tool catalog with a chain manifest.
	dir := t.TempDir()
	manifestsDir := filepath.Join(dir, "manifests")
	promptsDir := filepath.Join(dir, "prompts")
	bindingsDir := filepath.Join(dir, "bindings")
	for _, d := range []string{manifestsDir, promptsDir, bindingsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// providers.yaml: two openai providers; first has no env set (disabled), second configured.
	if err := os.WriteFile(filepath.Join(dir, "providers.yaml"), []byte(`providers:
  - id: chain-p1
    kind: openai
    optional: true
    apiURLEnv: TEST_CHAIN_P1_API_URL
    apiKeyEnv: TEST_CHAIN_P1_API_KEY
    modelEnv: TEST_CHAIN_P1_MODEL
    timeout: 5s
  - id: chain-p2
    kind: openai
    optional: true
    apiURLEnv: TEST_CHAIN_P2_API_URL
    apiKeyEnv: TEST_CHAIN_P2_API_KEY
    modelEnv: TEST_CHAIN_P2_MODEL
    timeout: 5s
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Manifest: chain of chain-p1 → chain-p2.
	if err := os.WriteFile(filepath.Join(manifestsDir, "test.chain.yaml"), []byte(`name: test.chain
providers:
  - provider: chain-p1
    providerToolName: openai.chat.completions.json
  - provider: chain-p2
    providerToolName: openai.chat.completions.json
fallback: on_disabled
description: chain test tool
deterministic: false
timeout: 5s
retryBudget: 0
modelPolicy: backend_workflow_json
promptFile: prompts/test.chain.prompt.tmpl
inputSchema:
  type: object
outputSchema:
  type: object
  required: [result]
  properties:
    result:
      type: string
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Minimal prompt template.
	if err := os.WriteFile(filepath.Join(promptsDir, "test.chain.prompt.tmpl"), []byte(`generate result`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Only chain-p2 is configured; chain-p1 has no env vars set.
	t.Setenv("TEST_CHAIN_P2_API_URL", fallback.URL)
	t.Setenv("TEST_CHAIN_P2_API_KEY", "test-key")
	t.Setenv("TEST_CHAIN_P2_MODEL", "test-model")

	catalog, err := tools.LoadCatalog(context.Background(), dir, nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	raw, err := catalog.Registry.Invoke(context.Background(), "test.chain", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("expected fallback to chain-p2; got error: %v", err)
	}
	var result struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Result != "ok-from-fallback" {
		t.Fatalf("unexpected result %q; want %q", result.Result, "ok-from-fallback")
	}
}

// TestLoadCatalog_ChainProvider_AllDisabledReturnsErrToolDisabled verifies that when
// every chain member is disabled, the chain returns ErrToolDisabled.
func TestLoadCatalog_ChainProvider_AllDisabledReturnsErrToolDisabled(t *testing.T) {
	dir := t.TempDir()
	manifestsDir := filepath.Join(dir, "manifests")
	promptsDir := filepath.Join(dir, "prompts")
	bindingsDir := filepath.Join(dir, "bindings")
	for _, d := range []string{manifestsDir, promptsDir, bindingsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Both providers have optional:true with no env vars set.
	if err := os.WriteFile(filepath.Join(dir, "providers.yaml"), []byte(`providers:
  - id: chain-all-p1
    kind: openai
    optional: true
    apiURLEnv: TEST_CHAIN_ALL_P1_API_URL
    apiKeyEnv: TEST_CHAIN_ALL_P1_API_KEY
    modelEnv: TEST_CHAIN_ALL_P1_MODEL
    timeout: 5s
  - id: chain-all-p2
    kind: openai
    optional: true
    apiURLEnv: TEST_CHAIN_ALL_P2_API_URL
    apiKeyEnv: TEST_CHAIN_ALL_P2_API_KEY
    modelEnv: TEST_CHAIN_ALL_P2_MODEL
    timeout: 5s
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(manifestsDir, "test.chain.all.yaml"), []byte(`name: test.chain.all
providers:
  - provider: chain-all-p1
    providerToolName: openai.chat.completions.json
  - provider: chain-all-p2
    providerToolName: openai.chat.completions.json
fallback: on_disabled
description: chain all-disabled test
deterministic: false
timeout: 5s
retryBudget: 0
modelPolicy: backend_workflow_json
promptFile: prompts/test.chain.all.prompt.tmpl
inputSchema:
  type: object
outputSchema:
  type: object
  required: [result]
  properties:
    result:
      type: string
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(promptsDir, "test.chain.all.prompt.tmpl"), []byte(`generate result`), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog, err := tools.LoadCatalog(context.Background(), dir, nil, tools.ModelToolConfig{})
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	_, err = catalog.Registry.Invoke(context.Background(), "test.chain.all", json.RawMessage(`{}`))
	if !errors.Is(err, tools.ErrToolDisabled) {
		t.Fatalf("expected ErrToolDisabled when all chain members disabled; got %v", err)
	}
}

func assertWelcomeEmailInvokeTool(t *testing.T, catalog *tools.LoadedCatalog, toolName string) {
	t.Helper()

	raw, err := catalog.Registry.Invoke(context.Background(), toolName, json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected valid JSON, got %s", raw)
	}
	var result struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Subject == "" || result.Body == "" {
		t.Fatalf("unexpected welcome email result: %s", raw)
	}
}
