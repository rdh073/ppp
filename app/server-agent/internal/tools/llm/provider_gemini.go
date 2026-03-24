package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// geminiProvider implements JSONModelClient using the Gemini
// generateContent API.
type geminiProvider struct {
	apiURL string
	apiKey string
	model  string
	http   *providerHTTPClient
	log    *slog.Logger
}

func newGeminiProvider(cfg ModelToolConfig, timeout time.Duration, log *slog.Logger) *geminiProvider {
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &geminiProvider{
		apiURL: apiURL,
		apiKey: strings.TrimSpace(cfg.APIKey),
		model:  strings.TrimSpace(cfg.Model),
		http:   newProviderHTTPClient(timeout),
		log:    log,
	}
}

func (p *geminiProvider) headers() map[string]string {
	if p.apiKey != "" {
		return map[string]string{"x-goog-api-key": p.apiKey}
	}
	return nil
}

func (p *geminiProvider) generateContentEndpoint() string {
	base := strings.TrimRight(p.apiURL, "/")
	if strings.Contains(base, ":generateContent") {
		return base
	}
	return base + "/models/" + url.PathEscape(p.model) + ":generateContent"
}

// ---- constructor functions (keep same signatures as old files) ----

// NewGeminiGenerateContentJSONClient returns a JSONModelClient backed by the
// Gemini generateContent API with JSON response schema enforcement.
func NewGeminiGenerateContentJSONClient(cfg ModelToolConfig, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return newGeminiProvider(cfg, 15*time.Second, log)
}

// wire types (JSON client)

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

type geminiGenerateContentRequest struct {
	SystemInstruction *geminiContent      `json:"systemInstruction,omitempty"`
	Contents          []geminiContent     `json:"contents"`
	GenerationConfig  geminiGenerationCfg `json:"generationConfig"`
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

// ---- JSONModelClient implementation ----

func (p *geminiProvider) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
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

	reqBody := geminiGenerateContentRequest{
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
	}

	start := time.Now()
	raw, err := p.http.PostJSON(ctx, p.generateContentEndpoint(), p.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if p.log != nil {
		p.log.Debug("gemini provider response",
			"toolName", request.ToolName,
			"elapsed", time.Since(start),
		)
	}

	var decoded geminiGenerateContentResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
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

// ---- internal helpers ----

func extractGeminiJSON(response geminiGenerateContentResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", errorf("%s", response.Error.Message)
	}
	if len(response.Candidates) == 0 {
		return "", errorf("no candidates in gemini response")
	}
	for _, part := range response.Candidates[0].Content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			return part.Text, nil
		}
	}
	return "", errorf("empty gemini content")
}
