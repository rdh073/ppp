package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

const (
	defaultLLMProductName = "AutoSDK"
	defaultLLMSenderName  = "AutoSDK"
)

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

// NewDefaultToolRegistry builds the production tool registry: deterministic
// local tools first, then model-backed tools behind the same boundary.
func NewDefaultToolRegistry(log *slog.Logger, cfg ModelToolConfig) ToolRegistry {
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

// NewModelToolRegistry registers the model-backed tools. A nil client keeps the
// tools visible to workflows but disabled, allowing explicit workflow fallback.
func NewModelToolRegistry(log *slog.Logger, client JSONModelClient) StaticToolRegistry {
	return NewStaticToolRegistry(ModelToolDefinitions(log, client)...)
}

func ModelToolDefinitions(log *slog.Logger, client JSONModelClient) []ToolDefinition {
	return []ToolDefinition{
		generateWelcomeEmailTool(log, client),
	}
}

func generateWelcomeEmailTool(log *slog.Logger, client JSONModelClient) ToolDefinition {
	manifest := ToolManifest{
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

	return ToolDefinition{
		Manifest:       manifest,
		ValidateParams: validateGenerateWelcomeEmailParams,
		ValidateResult: validateGeneratedWelcomeEmailResult,
		Handler: func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
			if client == nil {
				return nil, fmt.Errorf("%w: model client not configured", ErrToolDisabled)
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

func errorsIsRetryable(err error) bool {
	return errors.Is(err, ErrToolRetryable)
}
