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

type deepSeekChatCompletionsJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

type deepSeekChatCompletionsRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []deepSeekTool  `json:"tools"`
	ToolChoice  string          `json:"tool_choice"`
	Temperature float64         `json:"temperature"`
}

type deepSeekTool struct {
	Type     string               `json:"type"`
	Function deepSeekToolFunction `json:"function"`
}

type deepSeekToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters"`
	Strict      bool   `json:"strict,omitempty"`
}

type deepSeekChatCompletionsResponse struct {
	Choices []struct {
		Message struct {
			ToolCalls []struct {
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func NewDeepSeekChatCompletionsJSONClient(cfg ModelToolConfig, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return &deepSeekChatCompletionsJSONClient{
		apiURL: strings.TrimSpace(cfg.APIURL),
		apiKey: strings.TrimSpace(cfg.APIKey),
		model:  strings.TrimSpace(cfg.Model),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		log: log,
	}
}

func (c *deepSeekChatCompletionsJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
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

	body, err := json.Marshal(deepSeekChatCompletionsRequest{
		Model: c.model,
		Messages: []openAIMessage{
			{Role: "system", Content: defaultSystemPrompt(request.SystemPrompt)},
			{Role: "user", Content: request.Prompt},
		},
		Tools: []deepSeekTool{
			{
				Type: "function",
				Function: deepSeekToolFunction{
					Name:        schemaNameForTool(request.ToolName),
					Description: "Return the result as structured JSON matching the declared schema.",
					Parameters:  schema,
					Strict:      true,
				},
			},
		},
		ToolChoice:  "required",
		Temperature: temperature,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal deepseek request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build deepseek request: %w", err)
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
		return nil, MarkToolRetryable(fmt.Errorf("deepseek transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("read deepseek response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("deepseek provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, MarkToolRetryable(fmt.Errorf("deepseek provider status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("deepseek provider status %d: %s", resp.StatusCode, CompactProviderError(respBody))
	}

	var decoded deepSeekChatCompletionsResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode deepseek response: %w", err)
	}
	content, err := extractDeepSeekToolArguments(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract deepseek content: %w", err)
	}
	content = normalizeJSONPayload(content)
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("deepseek provider returned non-json content: %s", content)
	}
	return json.RawMessage(content), nil
}
