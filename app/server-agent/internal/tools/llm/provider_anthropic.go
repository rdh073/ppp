package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// timeNow and timeSince are thin wrappers to make call sites readable.
func timeNow() time.Time          { return time.Now() }
func timeSince(t time.Time) time.Duration { return time.Since(t) }

// anthropicProvider implements AgentLoop, JSONModelClient, and VisionModelClient
// using the Anthropic Messages API.
type anthropicProvider struct {
	apiURL  string
	apiKey  string
	model   string
	version string
	http    *providerHTTPClient
	log     *slog.Logger
}

func newAnthropicProvider(cfg ModelToolConfig, apiVersion string, timeout time.Duration, log *slog.Logger) *anthropicProvider {
	version := strings.TrimSpace(apiVersion)
	if version == "" {
		version = "2023-06-01"
	}
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://api.anthropic.com/v1/messages"
	}
	return &anthropicProvider{
		apiURL:  apiURL,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		model:   strings.TrimSpace(cfg.Model),
		version: version,
		http:    newProviderHTTPClient(timeout),
		log:     log,
	}
}

func (p *anthropicProvider) headers() map[string]string {
	h := map[string]string{
		"anthropic-version": p.version,
	}
	if p.apiKey != "" {
		h["x-api-key"] = p.apiKey
	}
	return h
}

// ---- constructor functions (keep same signatures as old files) ----

// NewAnthropicMessagesJSONClient creates a JSONModelClient backed by the Anthropic Messages API.
func NewAnthropicMessagesJSONClient(cfg ModelToolConfig, apiVersion string, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return newAnthropicProvider(cfg, apiVersion, 15*time.Second, log)
}

// NewAnthropicVisionClient creates a VisionModelClient backed by the Anthropic Messages API.
// Uses a longer HTTP timeout (vision inference is slower).
func NewAnthropicVisionClient(cfg ModelToolConfig, apiVersion string, log *slog.Logger) VisionModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return newAnthropicProvider(cfg, apiVersion, 45*time.Second, log)
}

// ---- wire types shared by all three implementations ----

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessagesRequest struct {
	Model       string              `json:"model"`
	System      string              `json:"system,omitempty"`
	MaxTokens   int                 `json:"max_tokens"`
	Messages    []anthropicMessage  `json:"messages"`
	Tools       []anthropicTool     `json:"tools"`
	ToolChoice  anthropicToolChoice `json:"tool_choice"`
	Temperature float64             `json:"temperature"`
}

type anthropicMessagesResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
		Name  string          `json:"name,omitempty"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Anthropic multimodal request wire types.
// anthropicVisionMsg differs from anthropicMessage in that Content is a typed
// slice (for image + text parts) rather than a plain string.

type anthropicVisionRequest struct {
	Model       string               `json:"model"`
	System      string               `json:"system,omitempty"`
	MaxTokens   int                  `json:"max_tokens"`
	Messages    []anthropicVisionMsg `json:"messages"`
	Tools       []anthropicTool      `json:"tools"`
	ToolChoice  anthropicToolChoice  `json:"tool_choice"`
	Temperature float64              `json:"temperature"`
}

type anthropicVisionMsg struct {
	Role    string                 `json:"role"`
	Content []anthropicContentPart `json:"content"`
}

type anthropicContentPart struct {
	Type   string              `json:"type"`             // "text" | "image"
	Text   string              `json:"text,omitempty"`
	Source *anthropicImgSource `json:"source,omitempty"` // present when Type=="image"
}

type anthropicImgSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg" | "image/png" | ...
	Data      string `json:"data"`       // raw base64 string (no data-URL prefix)
}

// ---- JSONModelClient implementation ----

func (p *anthropicProvider) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: model client not configured", ErrToolDisabled)
	}

	var schema any
	if len(request.OutputSchema) == 0 || !json.Valid(request.OutputSchema) {
		return nil, fmt.Errorf("invalid output schema for %s", request.ToolName)
	}
	if err := json.Unmarshal(request.OutputSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	temperature := 0.2
	if request.Temperature != nil {
		temperature = *request.Temperature
	}

	reqBody := anthropicMessagesRequest{
		Model:     p.model,
		System:    defaultSystemPrompt(request.SystemPrompt),
		MaxTokens: 1024,
		Messages: []anthropicMessage{
			{Role: "user", Content: request.Prompt},
		},
		Tools: []anthropicTool{
			{
				Name:        schemaNameForTool(request.ToolName),
				Description: "Return the result as structured JSON matching the declared schema.",
				InputSchema: schema,
			},
		},
		ToolChoice: anthropicToolChoice{
			Type: "tool",
			Name: schemaNameForTool(request.ToolName),
		},
		Temperature: temperature,
	}

	start := timeNow()
	raw, err := p.http.PostJSON(ctx, p.apiURL, p.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if p.log != nil {
		p.log.Debug("anthropic provider response",
			"toolName", request.ToolName,
			"elapsed", timeSince(start),
		)
	}

	var decoded anthropicMessagesResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	content, err := extractAnthropicToolInput(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract anthropic content: %w", err)
	}
	if !json.Valid(content) {
		return nil, fmt.Errorf("anthropic provider returned non-json content: %s", string(content))
	}
	return append(json.RawMessage(nil), content...), nil
}

// ---- VisionModelClient implementation ----

func (p *anthropicProvider) AnalyzeImage(ctx context.Context, req VisionAnalyzeRequest) (json.RawMessage, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: vision client not configured", ErrToolDisabled)
	}
	var schema any
	if len(req.OutputSchema) == 0 || !json.Valid(req.OutputSchema) {
		return nil, fmt.Errorf("invalid output schema for %s", req.ToolName)
	}
	if err := json.Unmarshal(req.OutputSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	toolName := schemaNameForTool(req.ToolName)
	reqBody := anthropicVisionRequest{
		Model:     p.model,
		MaxTokens: 1024,
		Messages: []anthropicVisionMsg{
			{
				Role: "user",
				Content: []anthropicContentPart{
					{
						Type: "image",
						Source: &anthropicImgSource{
							Type:      "base64",
							MediaType: req.MimeType,
							Data:      req.ImageBase64,
						},
					},
					{Type: "text", Text: req.Prompt},
				},
			},
		},
		Tools: []anthropicTool{
			{
				Name:        toolName,
				Description: "Return the analysis result as structured JSON matching the declared schema.",
				InputSchema: schema,
			},
		},
		ToolChoice:  anthropicToolChoice{Type: "tool", Name: toolName},
		Temperature: 0.1,
	}

	start := timeNow()
	raw, err := p.http.PostJSON(ctx, p.apiURL, p.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if p.log != nil {
		p.log.Debug("vision provider response",
			"toolName", req.ToolName,
			"elapsed", timeSince(start),
		)
	}

	var decoded anthropicMessagesResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode vision response: %w", err)
	}
	content, err := extractAnthropicToolInput(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract vision content: %w", err)
	}
	if !json.Valid(content) {
		return nil, fmt.Errorf("vision provider returned non-json: %s", string(content))
	}
	return append(json.RawMessage(nil), content...), nil
}

// ---- internal helpers ----

func extractAnthropicToolInput(response anthropicMessagesResponse) (json.RawMessage, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return nil, errorf("%s", response.Error.Message)
	}
	for _, part := range response.Content {
		if part.Type == "tool_use" && len(part.Input) > 0 {
			return append(json.RawMessage(nil), part.Input...), nil
		}
	}
	return nil, errorf("no tool_use content in anthropic response")
}
