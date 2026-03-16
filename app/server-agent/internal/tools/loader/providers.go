package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
	"github.com/autosdk/ppp/server-agent/internal/tools/vision"
)

const (
	anthropicMessagesJSONTool       = "anthropic.messages.json"
	geminiGenerateContentJSONTool   = "gemini.generate_content.json"
	deepSeekChatCompletionsJSONTool = "deepseek.chat.completions.json"
)

func loadProviders(ctx context.Context, dir string, log *slog.Logger, modelCfg llm.ModelToolConfig) (map[string]catalogProvider, error) {
	path := filepath.Join(dir, "providers.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read providers: %w", err)
	}
	var catalog providerCatalogFile
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parse providers: %w", err)
	}
	providers := make(map[string]catalogProvider, len(catalog.Providers))
	for _, cfg := range catalog.Providers {
		if cfg.ID == "" {
			return nil, fmt.Errorf("provider id required")
		}
		if _, exists := providers[cfg.ID]; exists {
			return nil, fmt.Errorf("duplicate provider id %s", cfg.ID)
		}
		switch strings.TrimSpace(cfg.Kind) {
		case "builtin":
			defs := make(map[string]tools.ToolDefinition)
			for _, def := range tools.LocalToolDefinitions() {
				defs[def.Manifest.Name] = def
			}
			providers[cfg.ID] = &builtinProvider{
				log:           log,
				modelCfg:      modelCfg,
				modelClient:   llm.NewOpenAICompatibleJSONClient(modelCfg, log),
				deterministic: defs,
			}
		case "http":
			baseURL := strings.TrimSpace(cfg.BaseURL)
			if cfg.BaseURLEnv != "" {
				if envValue := strings.TrimSpace(os.Getenv(cfg.BaseURLEnv)); envValue != "" {
					baseURL = envValue
				}
			}
			timeout := 10 * time.Second
			if strings.TrimSpace(cfg.Timeout) != "" {
				parsed, err := time.ParseDuration(strings.TrimSpace(cfg.Timeout))
				if err != nil {
					return nil, fmt.Errorf("provider %s invalid timeout: %w", cfg.ID, err)
				}
				timeout = parsed
			}
			provider := &httpProvider{
				cfg:        cfg,
				baseURL:    strings.TrimRight(baseURL, "/"),
				apiKey:     strings.TrimSpace(os.Getenv(cfg.APIKeyEnv)),
				httpClient: &http.Client{Timeout: timeout},
			}
			if provider.baseURL == "" {
				if !cfg.Optional {
					return nil, fmt.Errorf("provider %s baseURL required", cfg.ID)
				}
				provider.disabledReason = "baseURL not configured"
			} else if _, err := provider.discovery(ctx); err != nil {
				if !cfg.Optional {
					return nil, fmt.Errorf("provider %s discovery: %w", cfg.ID, err)
				}
				provider.disabledReason = "discovery failed: " + err.Error()
			}
			if provider.disabledReason != "" && log != nil {
				log.Warn("optional tool provider disabled", "provider", cfg.ID, "reason", provider.disabledReason)
			}
			providers[cfg.ID] = provider
		case "anthropic":
			provider, err := newPromptModelProvider(cfg, anthropicMessagesJSONTool, llm.NewAnthropicMessagesJSONClient, "https://api.anthropic.com/v1/messages", log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		case "gemini":
			provider, err := newPromptModelProvider(cfg, geminiGenerateContentJSONTool, func(cfg llm.ModelToolConfig, _ string, log *slog.Logger) llm.JSONModelClient {
				return llm.NewGeminiGenerateContentJSONClient(cfg, log)
			}, "https://generativelanguage.googleapis.com/v1beta", log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		case "openai":
			provider, err := newPromptModelProvider(cfg, builtinOpenAIJSONTool, func(cfg llm.ModelToolConfig, _ string, log *slog.Logger) llm.JSONModelClient {
				return llm.NewOpenAICompatibleJSONClient(cfg, log)
			}, "https://api.openai.com/v1/chat/completions", log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		case "deepseek":
			provider, err := newPromptModelProvider(cfg, deepSeekChatCompletionsJSONTool, func(cfg llm.ModelToolConfig, _ string, log *slog.Logger) llm.JSONModelClient {
				return llm.NewDeepSeekChatCompletionsJSONClient(cfg, log)
			}, "https://api.deepseek.com/chat/completions", log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		case "anthropic-vision":
			provider, err := newVisionModelProvider(cfg, log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		default:
			return nil, fmt.Errorf("unsupported provider kind %s", cfg.Kind)
		}
	}
	return providers, nil
}

// visionModelProvider implements catalogProvider for the "anthropic-vision" kind.
type visionModelProvider struct {
	cfg            providerConfig
	client         llm.VisionModelClient
	disabledReason string
}

func (p *visionModelProvider) Build(_ context.Context, m catalogToolManifest) (tools.ToolDefinition, error) {
	def := vision.NewVisionToolDefinition(m.Manifest, p.client)
	if p.disabledReason != "" {
		reason := p.disabledReason
		def.Handler = func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: %s", tools.ErrToolDisabled, reason)
		}
	}
	return def, nil
}

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
	client := llm.NewAnthropicVisionClient(modelCfg, resolveProviderString(cfg.APIVersion, cfg.APIVersionEnv), log)
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
