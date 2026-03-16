package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	
)

type geminiGenerateContentJSONClient struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
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

func (c *geminiGenerateContentJSONClient) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
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

	body, err := json.Marshal(geminiGenerateContentRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: defaultSystemPrompt(request.SystemPrompt)}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: request.Prompt}}},
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
		return nil, MarkToolRetryable(fmt.Errorf("gemini transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("read gemini response: %w", err))
	}

	if c.log != nil {
		c.log.Debug("gemini provider response",
			"toolName", request.ToolName,
			"status", resp.StatusCode,
			"elapsed", time.Since(start),
		)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, MarkToolRetryable(fmt.Errorf("gemini provider status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("gemini provider status %d: %s", resp.StatusCode, CompactProviderError(respBody))
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
