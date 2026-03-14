package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/tools"
)

type scriptedModelClient struct {
	results []json.RawMessage
	errs    []error
	calls   int
}

func (c *scriptedModelClient) GenerateJSON(_ context.Context, _ tools.JSONModelRequest) (json.RawMessage, error) {
	idx := c.calls
	c.calls++
	if idx < len(c.errs) && c.errs[idx] != nil {
		return nil, c.errs[idx]
	}
	if idx < len(c.results) {
		return c.results[idx], nil
	}
	return nil, errors.New("unexpected call")
}

func TestModelToolRegistry_InvokeRetriesTransientFailure(t *testing.T) {
	client := &scriptedModelClient{
		errs: []error{
			tools.MarkToolRetryable(errors.New("temporary provider error")),
		},
		results: []json.RawMessage{
			nil,
			json.RawMessage(`{"subject":"Selamat datang di AutoSDK","body":"Halo Ayu Lestari, akun AutoSDK Anda sudah siap digunakan. Gunakan email ini untuk melanjutkan proses verifikasi dan simpan kredensial Anda dengan aman.","language":"id","tone":"professional_warm"}`),
		},
	}
	registry := tools.NewModelToolRegistry(nil, client)

	raw, err := registry.Invoke(context.Background(), "content.generate_welcome_email", json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("expected 2 client calls, got %d", client.calls)
	}

	var result struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.Subject == "" || result.Body == "" {
		t.Fatal("expected subject and body in result")
	}
}

func TestDefaultToolRegistry_DisabledModelToolStillFailsClosed(t *testing.T) {
	registry := tools.NewDefaultToolRegistry(nil, tools.ModelToolConfig{})

	if _, ok := registry.Manifest("content.generate_welcome_email"); !ok {
		t.Fatal("expected disabled model tool manifest to remain visible")
	}

	_, err := registry.Invoke(context.Background(), "content.generate_welcome_email", json.RawMessage(`{"fullName":"Ayu Lestari"}`))
	if !errors.Is(err, tools.ErrToolDisabled) {
		t.Fatalf("expected ErrToolDisabled, got %v", err)
	}
}

func TestOpenAICompatibleJSONClient_GenerateJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected Authorization header %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "gpt-test" {
			t.Fatalf("unexpected model: %#v", payload["model"])
		}
		if _, ok := payload["response_format"]; !ok {
			t.Fatal("expected response_format in request")
		}

		_, _ = w.Write([]byte(`{
			"choices":[
				{
					"message":{
						"content":"{\"subject\":\"Selamat datang di AutoSDK\",\"body\":\"Halo Ayu Lestari, akun AutoSDK Anda sudah siap digunakan. Gunakan email ini untuk melanjutkan proses verifikasi dan simpan kredensial Anda dengan aman.\",\"language\":\"id\",\"tone\":\"professional_warm\"}"
					}
				}
			]
		}`))
	}))
	defer server.Close()

	client := tools.NewOpenAICompatibleJSONClient(tools.ModelToolConfig{
		APIURL: server.URL,
		APIKey: "test-key",
		Model:  "gpt-test",
	}, nil)
	raw, err := client.GenerateJSON(context.Background(), tools.JSONModelRequest{
		ToolName: "content.generate_welcome_email",
		Prompt:   "test prompt",
		OutputSchema: json.RawMessage(`{
			"type":"object",
			"required":["subject","body","language","tone"]
		}`),
	})
	if err != nil {
		t.Fatalf("GenerateJSON error: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected valid JSON, got %s", raw)
	}
	if !strings.Contains(string(raw), `"subject":"Selamat datang di AutoSDK"`) {
		t.Fatalf("unexpected response payload: %s", raw)
	}
}

func TestAnthropicMessagesJSONClient_GenerateJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("x-api-key"); got != "anthropic-key" {
			t.Fatalf("unexpected x-api-key %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("unexpected anthropic-version %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "claude-test" {
			t.Fatalf("unexpected model: %#v", payload["model"])
		}
		if _, ok := payload["tools"]; !ok {
			t.Fatal("expected tools in anthropic request")
		}
		if _, ok := payload["tool_choice"]; !ok {
			t.Fatal("expected tool_choice in anthropic request")
		}

		_, _ = w.Write([]byte(`{
			"content":[
				{
					"type":"tool_use",
					"name":"content_generate_welcome_email",
					"input":{"subject":"Selamat datang di AutoSDK","body":"Halo Ayu Lestari, akun AutoSDK Anda sudah siap digunakan. Gunakan email ini untuk melanjutkan proses verifikasi dan simpan kredensial Anda dengan aman.","language":"id","tone":"professional_warm"}
				}
			]
		}`))
	}))
	defer server.Close()

	client := tools.NewAnthropicMessagesJSONClient(tools.ModelToolConfig{
		APIURL: server.URL,
		APIKey: "anthropic-key",
		Model:  "claude-test",
	}, "", nil)
	raw, err := client.GenerateJSON(context.Background(), tools.JSONModelRequest{
		ToolName: "content.generate_welcome_email",
		Prompt:   "test prompt",
		OutputSchema: json.RawMessage(`{
			"type":"object",
			"required":["subject","body","language","tone"]
		}`),
	})
	if err != nil {
		t.Fatalf("GenerateJSON error: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected valid JSON, got %s", raw)
	}
	if !strings.Contains(string(raw), `"subject":"Selamat datang di AutoSDK"`) {
		t.Fatalf("unexpected response payload: %s", raw)
	}
}

func TestGeminiGenerateContentJSONClient_GenerateJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/models/gemini-test:generateContent" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "gemini-key" {
			t.Fatalf("unexpected x-goog-api-key %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		generationConfig, ok := payload["generationConfig"].(map[string]any)
		if !ok {
			t.Fatal("expected generationConfig in gemini request")
		}
		if generationConfig["responseMimeType"] != "application/json" {
			t.Fatalf("unexpected responseMimeType %#v", generationConfig["responseMimeType"])
		}
		if _, ok := generationConfig["responseSchema"]; !ok {
			t.Fatal("expected responseSchema in gemini request")
		}

		_, _ = w.Write([]byte(`{
			"candidates":[
				{
					"content":{
						"parts":[
							{
								"text":"{\"subject\":\"Selamat datang di AutoSDK\",\"body\":\"Halo Ayu Lestari, akun AutoSDK Anda sudah siap digunakan. Gunakan email ini untuk melanjutkan proses verifikasi dan simpan kredensial Anda dengan aman.\",\"language\":\"id\",\"tone\":\"professional_warm\"}"
							}
						]
					}
				}
			]
		}`))
	}))
	defer server.Close()

	client := tools.NewGeminiGenerateContentJSONClient(tools.ModelToolConfig{
		APIURL: server.URL,
		APIKey: "gemini-key",
		Model:  "gemini-test",
	}, nil)
	raw, err := client.GenerateJSON(context.Background(), tools.JSONModelRequest{
		ToolName: "content.generate_welcome_email",
		Prompt:   "test prompt",
		OutputSchema: json.RawMessage(`{
			"type":"object",
			"required":["subject","body","language","tone"]
		}`),
	})
	if err != nil {
		t.Fatalf("GenerateJSON error: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected valid JSON, got %s", raw)
	}
	if !strings.Contains(string(raw), `"subject":"Selamat datang di AutoSDK"`) {
		t.Fatalf("unexpected response payload: %s", raw)
	}
}

func TestDeepSeekChatCompletionsJSONClient_GenerateJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer deepseek-key" {
			t.Fatalf("unexpected Authorization header %q", got)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "deepseek-chat" {
			t.Fatalf("unexpected model: %#v", payload["model"])
		}
		if payload["tool_choice"] != "required" {
			t.Fatalf("unexpected tool_choice %#v", payload["tool_choice"])
		}
		toolsPayload, ok := payload["tools"].([]any)
		if !ok || len(toolsPayload) == 0 {
			t.Fatal("expected tools in deepseek request")
		}

		_, _ = w.Write([]byte(`{
			"choices":[
				{
					"message":{
						"tool_calls":[
							{
								"type":"function",
								"function":{
									"name":"content_generate_welcome_email",
									"arguments":"{\"subject\":\"Selamat datang di AutoSDK\",\"body\":\"Halo Ayu Lestari, akun AutoSDK Anda sudah siap digunakan. Gunakan email ini untuk melanjutkan proses verifikasi dan simpan kredensial Anda dengan aman.\",\"language\":\"id\",\"tone\":\"professional_warm\"}"
								}
							}
						]
					}
				}
			]
		}`))
	}))
	defer server.Close()

	client := tools.NewDeepSeekChatCompletionsJSONClient(tools.ModelToolConfig{
		APIURL: server.URL,
		APIKey: "deepseek-key",
		Model:  "deepseek-chat",
	}, nil)
	raw, err := client.GenerateJSON(context.Background(), tools.JSONModelRequest{
		ToolName: "content.generate_welcome_email",
		Prompt:   "test prompt",
		OutputSchema: json.RawMessage(`{
			"type":"object",
			"required":["subject","body","language","tone"]
		}`),
	})
	if err != nil {
		t.Fatalf("GenerateJSON error: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("expected valid JSON, got %s", raw)
	}
	if !strings.Contains(string(raw), `"subject":"Selamat datang di AutoSDK"`) {
		t.Fatalf("unexpected response payload: %s", raw)
	}
}
