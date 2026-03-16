package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	
)

type anthropicMessagesJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	version    string
	httpClient *http.Client
	log        *slog.Logger
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

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
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

func NewAnthropicMessagesJSONClient(cfg ModelToolConfig, apiVersion string, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	version := strings.TrimSpace(apiVersion)
	if version == "" {
		version = "2023-06-01"
	}
	return &anthropicMessagesJSONClient{
		apiURL:  strings.TrimSpace(cfg.APIURL),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		model:   strings.TrimSpace(cfg.Model),
		version: version,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		log: log,
	}
}

// NewAnthropicVisionClient creates a VisionModelClient backed by the Anthropic
// Messages API. Uses a longer HTTP timeout (vision inference is slower).
func NewAnthropicVisionClient(cfg ModelToolConfig, apiVersion string, log *slog.Logger) VisionModelClient {
	if !cfg.Enabled() {
		return nil
	}
	version := strings.TrimSpace(apiVersion)
	if version == "" {
		version = "2023-06-01"
	}
	return &anthropicMessagesJSONClient{
		apiURL:  strings.TrimSpace(cfg.APIURL),
		apiKey:  strings.TrimSpace(cfg.APIKey),
		model:   strings.TrimSpace(cfg.Model),
		version: version,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
		log: log,
	}
}

func (c *anthropicMessagesJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
	if c == nil {
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

	body, err := json.Marshal(anthropicMessagesRequest{
		Model:     c.model,
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
	})
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build anthropic request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", c.version)
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("anthropic transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("read anthropic response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("anthropic provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, MarkToolRetryable(fmt.Errorf("anthropic provider status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("anthropic provider status %d: %s", resp.StatusCode, CompactProviderError(respBody))
	}

	var decoded anthropicMessagesResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
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

// AnalyzeImage implements VisionModelClient on anthropicMessagesJSONClient.
func (c *anthropicMessagesJSONClient) AnalyzeImage(ctx context.Context, req VisionAnalyzeRequest) (json.RawMessage, error) {
	if c == nil {
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
	body, err := json.Marshal(anthropicVisionRequest{
		Model:     c.model,
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
	})
	if err != nil {
		return nil, fmt.Errorf("marshal vision request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build vision request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", c.version)
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("vision transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("read vision response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("vision provider response",
			"toolName", req.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, MarkToolRetryable(fmt.Errorf("vision provider status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("vision provider status %d: %s", resp.StatusCode, CompactProviderError(respBody))
	}

	var decoded anthropicMessagesResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
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
