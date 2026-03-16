package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/template"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

type promptModelProvider struct {
	cfg            providerConfig
	expectedTool   string
	modelClient    llm.JSONModelClient
	disabledReason string
}

type modelClientFactory func(llm.ModelToolConfig, string, *slog.Logger) llm.JSONModelClient

func newPromptModelProvider(cfg providerConfig, expectedTool string, factory modelClientFactory, defaultAPIURL string, log *slog.Logger) (*promptModelProvider, error) {
	modelCfg, disabledReason := providerModelConfig(cfg, defaultAPIURL)
	if disabledReason != "" && !cfg.Optional {
		return nil, fmt.Errorf("provider %s %s", cfg.ID, disabledReason)
	}
	provider := &promptModelProvider{
		cfg:            cfg,
		expectedTool:   expectedTool,
		disabledReason: disabledReason,
	}
	if disabledReason == "" {
		provider.modelClient = factory(modelCfg, resolveProviderString(cfg.APIVersion, cfg.APIVersionEnv), log)
		if provider.modelClient == nil {
			if !cfg.Optional {
				return nil, fmt.Errorf("provider %s model client not configured", cfg.ID)
			}
			provider.disabledReason = "model client not configured"
		}
	}
	if provider.disabledReason != "" && log != nil {
		log.Warn("optional model provider disabled", "provider", cfg.ID, "reason", provider.disabledReason)
	}
	return provider, nil
}

func (p *promptModelProvider) Build(_ context.Context, manifest catalogToolManifest) (tools.ToolDefinition, error) {
	return buildPromptModelToolDefinition(manifest, p.expectedTool, p.modelClient, p.disabledReason)
}

func buildPromptModelToolDefinition(manifest catalogToolManifest, expectedTool string, client llm.JSONModelClient, disabledReason string) (tools.ToolDefinition, error) {
	if manifest.Manifest.ProviderToolName != expectedTool {
		return tools.ToolDefinition{}, fmt.Errorf("unsupported prompt provider tool %s", manifest.Manifest.ProviderToolName)
	}
	if strings.TrimSpace(manifest.PromptTemplate) == "" {
		return tools.ToolDefinition{}, fmt.Errorf("prompt template required for %s", expectedTool)
	}
	validateParams, err := tools.CompileSchemaValidator(manifest.Manifest.InputSchema)
	if err != nil {
		return tools.ToolDefinition{}, err
	}
	validateResult, err := tools.CompileSchemaValidator(manifest.Manifest.OutputSchema)
	if err != nil {
		return tools.ToolDefinition{}, err
	}
	return tools.ToolDefinition{
		Manifest:       manifest.Manifest,
		ValidateParams: validateParams,
		ValidateResult: validateResult,
		Handler: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			if client == nil {
				reason := strings.TrimSpace(disabledReason)
				if reason == "" {
					reason = "model client not configured"
				}
				return nil, fmt.Errorf("%w: %s", tools.ErrToolDisabled, reason)
			}
			prompt, systemPrompt, err := renderPromptTemplates(manifest, params)
			if err != nil {
				return nil, fmt.Errorf("render prompt: %w", err)
			}
			request := llm.JSONModelRequest{
				ToolName:     manifest.Manifest.Name,
				SystemPrompt: systemPrompt,
				Prompt:       prompt,
				OutputSchema: manifest.Manifest.OutputSchema,
			}
			return client.GenerateJSON(ctx, request)
		},
	}, nil
}

func renderPromptTemplates(manifest catalogToolManifest, params json.RawMessage) (string, string, error) {
	var payload any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &payload); err != nil {
			return "", "", err
		}
	}
	contextData := struct {
		Params      any
		ToolName    string
		ModelPolicy string
	}{
		Params:      payload,
		ToolName:    manifest.Manifest.Name,
		ModelPolicy: manifest.ModelPolicy,
	}
	prompt, err := executePromptTemplate("prompt:"+manifest.Manifest.Name, manifest.PromptTemplate, contextData)
	if err != nil {
		return "", "", err
	}
	systemPrompt := strings.TrimSpace(manifest.SystemPrompt)
	if systemPrompt != "" {
		rendered, err := executePromptTemplate("system:"+manifest.Manifest.Name, manifest.SystemPrompt, contextData)
		if err != nil {
			return "", "", err
		}
		systemPrompt = rendered
	}
	return strings.TrimSpace(prompt), strings.TrimSpace(systemPrompt), nil
}

func executePromptTemplate(name, source string, data any) (string, error) {
	tpl, err := templateWithJSONFuncs(name).Parse(source)
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func templateWithJSONFuncs(name string) *template.Template {
	return template.New(name).Option("missingkey=error").Funcs(template.FuncMap{
		"json": func(v any) (string, error) {
			raw, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
		"default": func(defaultValue any, value any) any {
			switch typed := value.(type) {
			case string:
				if strings.TrimSpace(typed) == "" {
					return defaultValue
				}
				return typed
			case nil:
				return defaultValue
			default:
				return value
			}
		},
		"lower": strings.ToLower,
		"upper": strings.ToUpper,
		"trim":  strings.TrimSpace,
	})
}

func providerModelConfig(cfg providerConfig, defaultAPIURL string) (llm.ModelToolConfig, string) {
	apiURL := resolveProviderString(cfg.APIURL, cfg.APIURLEnv)
	if apiURL == "" {
		apiURL = strings.TrimSpace(defaultAPIURL)
	}
	model := resolveProviderString(cfg.Model, cfg.ModelEnv)
	apiKey := strings.TrimSpace(os.Getenv(cfg.APIKeyEnv))
	disabled := ""
	switch {
	case apiURL == "" && model == "":
		disabled = "apiURL and model not configured"
	case apiURL == "":
		disabled = "apiURL not configured"
	case model == "":
		disabled = "model not configured"
	}
	return llm.ModelToolConfig{
		APIURL: apiURL,
		APIKey: apiKey,
		Model:  model,
	}, disabled
}

func resolveProviderString(value string, envName string) string {
	if strings.TrimSpace(envName) != "" {
		if envValue := strings.TrimSpace(os.Getenv(envName)); envValue != "" {
			return envValue
		}
	}
	return strings.TrimSpace(value)
}
