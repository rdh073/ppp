package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/tools"
)

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
