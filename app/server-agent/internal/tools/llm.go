package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

const (
	defaultLLMProductName = "AutoSDK"
	defaultLLMSenderName  = "AutoSDK"
)

// ModelToolConfig contains the runtime wiring for model-backed tools.
type ModelToolConfig struct {
	APIURL string
	APIKey string
	Model  string
}

func (c ModelToolConfig) Enabled() bool {
	return strings.TrimSpace(c.APIURL) != "" && strings.TrimSpace(c.Model) != ""
}

// JSONModelRequest is the provider-neutral contract used by model-backed tools.
type JSONModelRequest struct {
	ToolName     string
	SystemPrompt string
	Prompt       string
	OutputSchema json.RawMessage
	Temperature  *float64
}

// JSONModelClient generates a JSON object that satisfies the supplied schema.
type JSONModelClient interface {
	GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error)
}

type openAICompatibleJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

type anthropicMessagesJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	version    string
	httpClient *http.Client
	log        *slog.Logger
}

type geminiGenerateContentJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

type deepSeekChatCompletionsJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

type generateWelcomeEmailParams struct {
	FullName    string `json:"fullName"`
	Email       string `json:"email,omitempty"`
	ProductName string `json:"productName,omitempty"`
	SenderName  string `json:"senderName,omitempty"`
	Language    string `json:"language,omitempty"`
	Tone        string `json:"tone,omitempty"`
}

type generatedWelcomeEmailResult struct {
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Language string `json:"language"`
	Tone     string `json:"tone"`
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

type geminiGenerateContentRequest struct {
	SystemInstruction *geminiContent      `json:"systemInstruction,omitempty"`
	Contents          []geminiContent     `json:"contents"`
	GenerationConfig  geminiGenerationCfg `json:"generationConfig"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationCfg struct {
	ResponseMIMEType string  `json:"responseMimeType"`
	ResponseSchema   any     `json:"responseSchema,omitempty"`
	Temperature      float64 `json:"temperature"`
}

type geminiGenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
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

// NewDefaultToolRegistry builds the production tool registry: deterministic
// local tools first, then model-backed tools behind the same boundary.
func NewDefaultToolRegistry(log *slog.Logger, cfg ModelToolConfig) nodes.ToolRegistry {
	local := NewLocalToolRegistry()

	var client JSONModelClient
	if cfg.Enabled() {
		client = NewOpenAICompatibleJSONClient(cfg, log)
		if log != nil {
			log.Info("model-backed tools enabled", "model", cfg.Model, "apiUrl", cfg.APIURL)
		}
	} else if log != nil {
		log.Info("model-backed tools disabled", "reason", "AUTO_TOOL_LLM_API_URL or AUTO_TOOL_LLM_MODEL not set")
	}

	return NewCompositeToolRegistry(local, NewModelToolRegistry(log, client))
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

func NewGeminiGenerateContentJSONClient(cfg ModelToolConfig, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return &geminiGenerateContentJSONClient{
		apiURL: strings.TrimSpace(cfg.APIURL),
		apiKey: strings.TrimSpace(cfg.APIKey),
		model:  strings.TrimSpace(cfg.Model),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		log: log,
	}
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

// NewModelToolRegistry registers the model-backed tools. A nil client keeps the
// tools visible to workflows but disabled, allowing explicit workflow fallback.
func NewModelToolRegistry(log *slog.Logger, client JSONModelClient) nodes.StaticToolRegistry {
	return nodes.NewStaticToolRegistry(ModelToolDefinitions(log, client)...)
}

func ModelToolDefinitions(log *slog.Logger, client JSONModelClient) []nodes.ToolDefinition {
	return []nodes.ToolDefinition{
		generateWelcomeEmailTool(log, client),
	}
}

func generateWelcomeEmailTool(log *slog.Logger, client JSONModelClient) nodes.ToolDefinition {
	manifest := nodes.ToolManifest{
		Name:          "content.generate_welcome_email",
		Description:   "Generates a short welcome email body from profile fields",
		Deterministic: false,
		Timeout:       8 * time.Second,
		RetryBudget:   1,
		InputSchema: rawSchema(`{
			"type":"object",
			"required":["fullName"],
			"properties":{
				"fullName":{"type":"string"},
				"email":{"type":"string"},
				"productName":{"type":"string"},
				"senderName":{"type":"string"},
				"language":{"enum":["id","en"]},
				"tone":{"type":"string"}
			}
		}`),
		OutputSchema: rawSchema(`{
			"type":"object",
			"required":["subject","body","language","tone"],
			"properties":{
				"subject":{"type":"string"},
				"body":{"type":"string"},
				"language":{"enum":["id","en"]},
				"tone":{"type":"string"}
			}
		}`),
	}

	return nodes.ToolDefinition{
		Manifest:       manifest,
		ValidateParams: validateGenerateWelcomeEmailParams,
		ValidateResult: validateGeneratedWelcomeEmailResult,
		Handler: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			if client == nil {
				return nil, fmt.Errorf("%w: model client not configured", nodes.ErrToolDisabled)
			}

			params, err := parseGenerateWelcomeEmailParams(raw)
			if err != nil {
				return nil, err
			}

			request := JSONModelRequest{
				ToolName:     manifest.Name,
				Prompt:       buildWelcomeEmailPrompt(params),
				OutputSchema: manifest.OutputSchema,
			}

			start := time.Now()
			if log != nil {
				log.Debug("invoking model-backed tool",
					"toolName", manifest.Name,
					"fullName", params.FullName,
					"language", params.Language,
					"tone", params.Tone,
				)
			}
			result, err := client.GenerateJSON(ctx, request)
			if err != nil {
				if log != nil {
					log.Warn("model-backed tool failed",
						"toolName", manifest.Name,
						"retryable", errorsIsRetryable(err),
						"elapsed", time.Since(start),
						"err", err,
					)
				}
				return nil, err
			}
			if log != nil {
				log.Debug("model-backed tool completed",
					"toolName", manifest.Name,
					"elapsed", time.Since(start),
				)
			}
			return result, nil
		},
	}
}

func validateGenerateWelcomeEmailParams(raw json.RawMessage) error {
	_, err := parseGenerateWelcomeEmailParams(raw)
	return err
}

func parseGenerateWelcomeEmailParams(raw json.RawMessage) (generateWelcomeEmailParams, error) {
	var params generateWelcomeEmailParams
	if len(raw) == 0 {
		return params, fmt.Errorf("fullName is required")
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, fmt.Errorf("decode params: %w", err)
	}
	params.FullName = strings.TrimSpace(params.FullName)
	params.Email = strings.TrimSpace(params.Email)
	params.ProductName = strings.TrimSpace(params.ProductName)
	params.SenderName = strings.TrimSpace(params.SenderName)
	params.Language = strings.TrimSpace(params.Language)
	params.Tone = strings.TrimSpace(params.Tone)

	if params.FullName == "" {
		return params, fmt.Errorf("fullName is required")
	}
	if params.Email != "" && !strings.Contains(params.Email, "@") {
		return params, fmt.Errorf("email must contain @")
	}
	if params.ProductName == "" {
		params.ProductName = defaultLLMProductName
	}
	if params.SenderName == "" {
		params.SenderName = defaultLLMSenderName
	}
	switch params.Language {
	case "", "id":
		params.Language = "id"
	case "en":
	default:
		return params, fmt.Errorf("language must be id or en")
	}
	if params.Tone == "" {
		params.Tone = "professional_warm"
	}
	return params, nil
}

func validateGeneratedWelcomeEmailResult(raw json.RawMessage) error {
	var result generatedWelcomeEmailResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	if strings.TrimSpace(result.Subject) == "" {
		return fmt.Errorf("subject is required")
	}
	if strings.TrimSpace(result.Body) == "" {
		return fmt.Errorf("body is required")
	}
	switch strings.TrimSpace(result.Language) {
	case "id", "en":
	default:
		return fmt.Errorf("language must be id or en")
	}
	if strings.TrimSpace(result.Tone) == "" {
		return fmt.Errorf("tone is required")
	}
	return nil
}

func buildWelcomeEmailPrompt(params generateWelcomeEmailParams) string {
	languageInstruction := "Write the email in Indonesian."
	if params.Language == "en" {
		languageInstruction = "Write the email in English."
	}
	emailLine := "No explicit email address was provided."
	if params.Email != "" {
		emailLine = "The email address to mention is " + params.Email + "."
	}

	return strings.TrimSpace(fmt.Sprintf(`
You are a workflow tool that returns JSON only.

Generate a short welcome email for a newly created profile.
%s

Profile:
- Full name: %s
- Product: %s
- Sender: %s
- Tone: %s
- %s

Requirements:
- Return JSON only with keys subject, body, language, tone.
- Subject must be concise and human-readable.
- Body must be plain text with no markdown.
- Body should be 80 to 180 words.
- Do not invent facts beyond the provided profile fields.
`, languageInstruction, params.FullName, params.ProductName, params.SenderName, params.Tone, emailLine))
}

func (c *openAICompatibleJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: model client not configured", nodes.ErrToolDisabled)
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
			{
				Role:    "system",
				Content: defaultSystemPrompt(request.SystemPrompt),
			},
			{
				Role:    "user",
				Content: request.Prompt,
			},
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
		return nil, nodes.MarkToolRetryable(fmt.Errorf("llm transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nodes.MarkToolRetryable(fmt.Errorf("read llm response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("llm provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, nodes.MarkToolRetryable(fmt.Errorf("llm provider status %d: %s", resp.StatusCode, compactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("llm provider status %d: %s", resp.StatusCode, compactProviderError(respBody))
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

func (c *geminiGenerateContentJSONClient) endpoint() string {
	base := strings.TrimRight(strings.TrimSpace(c.apiURL), "/")
	if strings.Contains(base, ":generateContent") {
		return base
	}
	if base == "" {
		return ""
	}
	return base + "/models/" + url.PathEscape(c.model) + ":generateContent"
}

func (c *anthropicMessagesJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: model client not configured", nodes.ErrToolDisabled)
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
		return nil, nodes.MarkToolRetryable(fmt.Errorf("anthropic transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nodes.MarkToolRetryable(fmt.Errorf("read anthropic response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("anthropic provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, nodes.MarkToolRetryable(fmt.Errorf("anthropic provider status %d: %s", resp.StatusCode, compactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("anthropic provider status %d: %s", resp.StatusCode, compactProviderError(respBody))
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

func (c *geminiGenerateContentJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: model client not configured", nodes.ErrToolDisabled)
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

	body, err := json.Marshal(geminiGenerateContentRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: defaultSystemPrompt(request.SystemPrompt)}},
		},
		Contents: []geminiContent{
			{
				Parts: []geminiPart{{Text: request.Prompt}},
			},
		},
		GenerationConfig: geminiGenerationCfg{
			ResponseMIMEType: "application/json",
			ResponseSchema:   schema,
			Temperature:      temperature,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", c.apiKey)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nodes.MarkToolRetryable(fmt.Errorf("gemini transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nodes.MarkToolRetryable(fmt.Errorf("read gemini response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("gemini provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, nodes.MarkToolRetryable(fmt.Errorf("gemini provider status %d: %s", resp.StatusCode, compactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("gemini provider status %d: %s", resp.StatusCode, compactProviderError(respBody))
	}

	var decoded geminiGenerateContentResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}
	content, err := extractGeminiJSON(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract gemini content: %w", err)
	}
	content = normalizeJSONPayload(content)
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("gemini provider returned non-json content: %s", content)
	}
	return json.RawMessage(content), nil
}

func (c *deepSeekChatCompletionsJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: model client not configured", nodes.ErrToolDisabled)
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
			{
				Role:    "system",
				Content: defaultSystemPrompt(request.SystemPrompt),
			},
			{
				Role:    "user",
				Content: request.Prompt,
			},
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
		return nil, nodes.MarkToolRetryable(fmt.Errorf("deepseek transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nodes.MarkToolRetryable(fmt.Errorf("read deepseek response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("deepseek provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, nodes.MarkToolRetryable(fmt.Errorf("deepseek provider status %d: %s", resp.StatusCode, compactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("deepseek provider status %d: %s", resp.StatusCode, compactProviderError(respBody))
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

func extractChoiceContent(response openAIChatCompletionsResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", fmt.Errorf("%s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in llm response")
	}
	raw := response.Choices[0].Message.Content
	if len(raw) == 0 {
		return "", fmt.Errorf("empty llm content")
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
			return "", fmt.Errorf("empty llm content parts")
		}
		return b.String(), nil
	}

	return "", fmt.Errorf("unsupported llm content shape")
}

func extractAnthropicToolInput(response anthropicMessagesResponse) (json.RawMessage, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return nil, fmt.Errorf("%s", response.Error.Message)
	}
	for _, part := range response.Content {
		if part.Type == "tool_use" && len(part.Input) > 0 {
			return append(json.RawMessage(nil), part.Input...), nil
		}
	}
	return nil, fmt.Errorf("no tool_use content in anthropic response")
}

func extractGeminiJSON(response geminiGenerateContentResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", fmt.Errorf("%s", response.Error.Message)
	}
	if len(response.Candidates) == 0 {
		return "", fmt.Errorf("no candidates in gemini response")
	}
	for _, part := range response.Candidates[0].Content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			return part.Text, nil
		}
	}
	return "", fmt.Errorf("empty gemini content")
}

func extractDeepSeekToolArguments(response deepSeekChatCompletionsResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", fmt.Errorf("%s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices in deepseek response")
	}
	for _, toolCall := range response.Choices[0].Message.ToolCalls {
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			return toolCall.Function.Arguments, nil
		}
	}
	return "", fmt.Errorf("no tool call arguments in deepseek response")
}

func normalizeJSONPayload(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func schemaNameForTool(toolName string) string {
	var b strings.Builder
	for _, r := range toolName {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		return "tool_output"
	}
	return name
}

func compactProviderError(raw []byte) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return http.StatusText(http.StatusBadGateway)
	}
	var payload struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return strings.TrimSpace(payload.Error.Message)
	}
	return trimmed
}

func errorsIsRetryable(err error) bool {
	return errors.Is(err, nodes.ErrToolRetryable)
}

func defaultSystemPrompt(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "You are a backend workflow tool. Return a single JSON object that satisfies the schema exactly."
	}
	return value
}
