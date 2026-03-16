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

type openAICompatibleJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

type openAIChatCompletionsRequest struct {
	Model          string               `json:"model"`
	Messages       []openAIMessage      `json:"messages"`
	ResponseFormat openAIResponseFormat `json:"response_format"`
	Temperature    float64              `json:"temperature"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type       string           `json:"type"`
	JSONSchema openAIJSONSchema `json:"json_schema"`
}

type openAIJSONSchema struct {
	Name   string `json:"name"`
	Strict bool   `json:"strict"`
	Schema any    `json:"schema"`
}

type openAIChatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewOpenAICompatibleJSONClient targets a chat-completions style endpoint used
// by OpenAI-compatible providers. No external SDK is required.
func NewOpenAICompatibleJSONClient(cfg ModelToolConfig, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return &openAICompatibleJSONClient{
		apiURL: strings.TrimSpace(cfg.APIURL),
		apiKey: strings.TrimSpace(cfg.APIKey),
		model:  strings.TrimSpace(cfg.Model),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		log: log,
	}
}

func (c *openAICompatibleJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
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

	body, err := json.Marshal(openAIChatCompletionsRequest{
		Model: c.model,
		Messages: []openAIMessage{
			{Role: "system", Content: defaultSystemPrompt(request.SystemPrompt)},
			{Role: "user", Content: request.Prompt},
		},
		ResponseFormat: openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: openAIJSONSchema{
				Name:   schemaNameForTool(request.ToolName),
				Strict: true,
				Schema: schema,
			},
		},
		Temperature: temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal llm request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build llm request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("llm transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("read llm response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("llm provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, MarkToolRetryable(fmt.Errorf("llm provider status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("llm provider status %d: %s", resp.StatusCode, CompactProviderError(respBody))
	}

	var decoded openAIChatCompletionsResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode llm response: %w", err)
	}
	content, err := extractChoiceContent(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract llm content: %w", err)
	}
	content = normalizeJSONPayload(content)
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("provider returned non-json content: %s", content)
	}
	return json.RawMessage(content), nil
}
