package tools

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

// VisionModelClient sends an image + runtime prompt to a vision LLM and returns
// structured JSON that satisfies the caller-supplied output schema.
// The interface is owned here (inner layer) and implemented by provider adapters.
type VisionModelClient interface {
	AnalyzeImage(ctx context.Context, req VisionAnalyzeRequest) (json.RawMessage, error)
}

// VisionAnalyzeRequest carries all runtime parameters for one vision call.
// ToolName is used for schema naming in forced tool-use mode.
type VisionAnalyzeRequest struct {
	ToolName     string
	ImageBase64  string
	MimeType     string          // e.g. "image/jpeg"
	Prompt       string
	OutputSchema json.RawMessage // caller-declared output shape
}

// visionAnalyzeParams is the string-map-compatible input shape produced by the
// workflow engine: every ToolCallDef.Params value is a string after interpolation,
// so json.Marshal(map[string]string{...}) produces {"key":"string-value"}.
// The outputSchema field carries a JSON-encoded schema string so it survives the
// string-map round-trip; parseVisionAnalyzeParams decodes it back to RawMessage.
type visionAnalyzeParams struct {
	ImageBase64  string `json:"imageBase64"`
	MimeType     string `json:"mimeType,omitempty"`
	Prompt       string `json:"prompt"`
	OutputSchema string `json:"outputSchema"` // JSON-encoded schema string
}

// visionModelProvider implements catalogProvider for the "anthropic-vision" kind.
// prompt and outputSchema are NOT baked into the provider — they arrive at runtime
// via workflow params, keeping every aspect of the analysis data-driven from YAML.
type visionModelProvider struct {
	cfg            providerConfig
	client         VisionModelClient
	disabledReason string
}

// Build returns the ToolDefinition using the manifest loaded from the manifest
// YAML file. The manifest owns name, description, schemas, timeout, retryBudget.
func (p *visionModelProvider) Build(_ context.Context, m catalogToolManifest) (ToolDefinition, error) {
	def := visionAnalyzeTool(m.Manifest, p.client)
	if p.disabledReason != "" {
		reason := p.disabledReason
		def.Handler = func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: %s", ErrToolDisabled, reason)
		}
	}
	return def, nil
}

// visionAnalyzeTool builds a ToolDefinition whose manifest is entirely sourced
// from the YAML catalog. The handler routes each call through client.AnalyzeImage
// with runtime prompt and outputSchema extracted from the workflow params.
func visionAnalyzeTool(manifest ToolManifest, client VisionModelClient) ToolDefinition {
	return ToolDefinition{
		Manifest:       manifest,
		ValidateParams: validateVisionAnalyzeParams,
		// ValidateResult is intentionally nil: output shape is declared at runtime
		// by the workflow-supplied outputSchema, not by a static manifest schema.
		Handler: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			if client == nil {
				return nil, fmt.Errorf("%w: vision model client not configured", ErrToolDisabled)
			}
			params, schema, err := parseVisionAnalyzeParams(raw)
			if err != nil {
				return nil, err
			}
			return client.AnalyzeImage(ctx, VisionAnalyzeRequest{
				ToolName:     manifest.Name,
				ImageBase64:  params.ImageBase64,
				MimeType:     params.MimeType,
				Prompt:       params.Prompt,
				OutputSchema: schema,
			})
		},
	}
}

func validateVisionAnalyzeParams(raw json.RawMessage) error {
	_, _, err := parseVisionAnalyzeParams(raw)
	return err
}

func parseVisionAnalyzeParams(raw json.RawMessage) (visionAnalyzeParams, json.RawMessage, error) {
	var params visionAnalyzeParams
	if len(raw) == 0 {
		return params, nil, fmt.Errorf("imageBase64, prompt, and outputSchema are required")
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return params, nil, fmt.Errorf("decode params: %w", err)
	}
	params.ImageBase64 = strings.TrimSpace(params.ImageBase64)
	params.Prompt = strings.TrimSpace(params.Prompt)
	params.OutputSchema = strings.TrimSpace(params.OutputSchema)
	if params.ImageBase64 == "" {
		return params, nil, fmt.Errorf("imageBase64 is required")
	}
	if params.Prompt == "" {
		return params, nil, fmt.Errorf("prompt is required")
	}
	if params.OutputSchema == "" {
		return params, nil, fmt.Errorf("outputSchema is required")
	}
	if !json.Valid([]byte(params.OutputSchema)) {
		return params, nil, fmt.Errorf("outputSchema must be valid JSON")
	}
	if params.MimeType == "" {
		params.MimeType = "image/jpeg"
	}
	return params, json.RawMessage(params.OutputSchema), nil
}

// NewAnthropicVisionClient creates a VisionModelClient backed by the Anthropic
// Messages API. Separate from NewAnthropicMessagesJSONClient because it sets a
// longer HTTP timeout (vision inference is slower than text) and the return type
// must satisfy VisionModelClient rather than JSONModelClient.
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

// newVisionModelProvider builds a visionModelProvider from catalog config.
// Uses the same providerModelConfig helper as the text providers, defaulting to
// the standard Anthropic Messages endpoint.
func newVisionModelProvider(cfg providerConfig, log *slog.Logger) (*visionModelProvider, error) {
	modelCfg, disabledReason := providerModelConfig(cfg, "https://api.anthropic.com/v1/messages")
	provider := &visionModelProvider{cfg: cfg, disabledReason: disabledReason}
	if disabledReason != "" {
		if !cfg.Optional {
			return nil, fmt.Errorf("provider %s %s", cfg.ID, disabledReason)
		}
		if log != nil {
			log.Warn("optional vision provider disabled", "provider", cfg.ID, "reason", disabledReason)
		}
		return provider, nil
	}
	client := NewAnthropicVisionClient(modelCfg, resolveProviderString(cfg.APIVersion, cfg.APIVersionEnv), log)
	if client == nil {
		if !cfg.Optional {
			return nil, fmt.Errorf("provider %s vision client not configured", cfg.ID)
		}
		provider.disabledReason = "vision client not configured"
		if log != nil {
			log.Warn("optional vision provider disabled", "provider", cfg.ID, "reason", provider.disabledReason)
		}
	} else {
		provider.client = client
	}
	return provider, nil
}

// AnalyzeImage implements VisionModelClient on anthropicMessagesJSONClient.
// Sends the image as base64 alongside the prompt using forced tool-use so the
// response is always a structured JSON object that satisfies OutputSchema.
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
		return nil, MarkToolRetryable(fmt.Errorf("vision provider status %d: %s", resp.StatusCode, compactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("vision provider status %d: %s", resp.StatusCode, compactProviderError(respBody))
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
	Role    string               `json:"role"`
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
