package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

// visionAnalyzeParams is the string-map-compatible input shape produced by the
// workflow engine. The outputSchema field carries a JSON-encoded schema string
// so it survives the string-map round-trip; Parse decodes it back to RawMessage.
type visionAnalyzeParams struct {
	ImageBase64  string `json:"imageBase64"`
	MimeType     string `json:"mimeType,omitempty"`
	Prompt       string `json:"prompt"`
	OutputSchema string `json:"outputSchema"` // JSON-encoded schema string
}

// NewVisionToolDefinition builds a ToolDefinition whose manifest is entirely
// sourced from the YAML catalog. The handler routes each call through
// client.AnalyzeImage with runtime prompt and outputSchema from the params.
func NewVisionToolDefinition(manifest tools.ToolManifest, client llm.VisionModelClient) tools.ToolDefinition {
	return tools.ToolDefinition{
		Manifest:       manifest,
		ValidateParams: validateVisionAnalyzeParams,
		Handler: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			if client == nil {
				return nil, fmt.Errorf("%w: vision model client not configured", tools.ErrToolDisabled)
			}
			params, schema, err := parseVisionAnalyzeParams(raw)
			if err != nil {
				return nil, err
			}
			return client.AnalyzeImage(ctx, llm.VisionAnalyzeRequest{
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
