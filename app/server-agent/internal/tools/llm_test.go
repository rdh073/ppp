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
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
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
			nodes.MarkToolRetryable(errors.New("temporary provider error")),
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
	if !errors.Is(err, nodes.ErrToolDisabled) {
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
