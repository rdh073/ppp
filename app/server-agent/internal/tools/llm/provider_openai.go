package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// openAIProvider implements AgentLoop and JSONModelClient using the OpenAI
// chat-completions API. It is also used for DeepSeek (same wire format).
type openAIProvider struct {
	apiURL string
	apiKey string
	model  string
	http   *providerHTTPClient
	log    *slog.Logger
}

func newOpenAIProvider(cfg ModelToolConfig, timeout time.Duration, log *slog.Logger) *openAIProvider {
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1/chat/completions"
	}
	return &openAIProvider{
		apiURL: apiURL,
		apiKey: strings.TrimSpace(cfg.APIKey),
		model:  strings.TrimSpace(cfg.Model),
		http:   newProviderHTTPClient(timeout),
		log:    log,
	}
}

func (p *openAIProvider) headers() map[string]string {
	if p.apiKey != "" {
		return map[string]string{"Authorization": "Bearer " + p.apiKey}
	}
	return nil
}

// ---- constructor functions (keep same signatures as old files) ----

// NewOpenAICompatibleJSONClient targets a chat-completions style endpoint used
// by OpenAI-compatible providers. No external SDK is required.
func NewOpenAICompatibleJSONClient(cfg ModelToolConfig, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return newOpenAIProvider(cfg, 15*time.Second, log)
}

// NewDeepSeekChatCompletionsJSONClient returns a JSONModelClient backed by the
// DeepSeek chat-completions API (tool-call forced JSON extraction).
func NewDeepSeekChatCompletionsJSONClient(cfg ModelToolConfig, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	p := newOpenAIProvider(cfg, 15*time.Second, log)
	return &deepSeekJSONClient{openAIProvider: p}
}

// ---- wire types ----

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatCompletionsRequest struct {
	Model          string               `json:"model"`
	Messages       []openAIMessage      `json:"messages"`
	ResponseFormat openAIResponseFormat `json:"response_format"`
	Temperature    float64              `json:"temperature"`
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

// DeepSeek tool-call wire types (used by deepSeekJSONClient)

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

type deepSeekChatCompletionsRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Tools       []deepSeekTool  `json:"tools"`
	ToolChoice  string          `json:"tool_choice"`
	Temperature float64         `json:"temperature"`
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

// ---- JSONModelClient implementation (OpenAI structured output) ----

func (p *openAIProvider) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
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

	reqBody := openAIChatCompletionsRequest{
		Model: p.model,
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
	}

	start := time.Now()
	raw, err := p.http.PostJSON(ctx, p.apiURL, p.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if p.log != nil {
		p.log.Debug("llm provider response",
			"toolName", request.ToolName,
			"elapsed", time.Since(start),
		)
	}

	var decoded openAIChatCompletionsResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
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

// ---- DeepSeek JSON client (tool-call extraction, different wire format) ----

// deepSeekJSONClient wraps openAIProvider but uses DeepSeek's tool-call
// request/response shape rather than OpenAI's structured-output response_format.
type deepSeekJSONClient struct {
	*openAIProvider
}

func (c *deepSeekJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
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

	reqBody := deepSeekChatCompletionsRequest{
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
	}

	start := time.Now()
	raw, err := c.http.PostJSON(ctx, c.apiURL, c.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if c.log != nil {
		c.log.Debug("deepseek provider response",
			"toolName", request.ToolName,
			"elapsed", time.Since(start),
		)
	}

	var decoded deepSeekChatCompletionsResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
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

// ---- internal helpers ----

func extractChoiceContent(response openAIChatCompletionsResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", errorf("%s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", errorf("no choices in llm response")
	}
	raw := response.Choices[0].Message.Content
	if len(raw) == 0 {
		return "", errorf("empty llm content")
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if strings.TrimSpace(part.Text) != "" {
				b.WriteString(part.Text)
			}
		}
		if b.Len() == 0 {
			return "", errorf("empty llm content parts")
		}
		return b.String(), nil
	}

	return "", errorf("unsupported llm content shape")
}

func extractDeepSeekToolArguments(response deepSeekChatCompletionsResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", errorf("%s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", errorf("no choices in deepseek response")
	}
	for _, toolCall := range response.Choices[0].Message.ToolCalls {
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			return toolCall.Function.Arguments, nil
		}
	}
	return "", errorf("no tool call arguments in deepseek response")
}
