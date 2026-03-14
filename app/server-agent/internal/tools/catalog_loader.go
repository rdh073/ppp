package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

type LoadedCatalog struct {
	Registry nodes.ToolRegistry
	Bindings nodes.ToolBindingResolver
}

type catalogProvider interface {
	Build(ctx context.Context, manifest catalogToolManifest) (nodes.ToolDefinition, error)
}

type providerCatalogFile struct {
	Providers []providerConfig `yaml:"providers"`
}

type providerConfig struct {
	ID            string `yaml:"id"`
	Kind          string `yaml:"kind"`
	Optional      bool   `yaml:"optional"`
	BaseURL       string `yaml:"baseURL"`
	BaseURLEnv    string `yaml:"baseURLEnv"`
	APIURL        string `yaml:"apiURL"`
	APIURLEnv     string `yaml:"apiURLEnv"`
	APIKeyEnv     string `yaml:"apiKeyEnv"`
	Model         string `yaml:"model"`
	ModelEnv      string `yaml:"modelEnv"`
	APIVersion    string `yaml:"apiVersion"`
	APIVersionEnv string `yaml:"apiVersionEnv"`
	Timeout       string `yaml:"timeout"`
}

type toolManifestConfig struct {
	Name             string   `yaml:"name"`
	Provider         string   `yaml:"provider"`
	ProviderToolName string   `yaml:"providerToolName"`
	Description      string   `yaml:"description"`
	Deterministic    *bool    `yaml:"deterministic"`
	Timeout          string   `yaml:"timeout"`
	RetryBudget      *int     `yaml:"retryBudget"`
	InputSchema      any      `yaml:"inputSchema"`
	InputSchemaFile  string   `yaml:"inputSchemaFile"`
	OutputSchema     any      `yaml:"outputSchema"`
	OutputSchemaFile string   `yaml:"outputSchemaFile"`
	PromptFile       string   `yaml:"promptFile"`
	SystemPromptFile string   `yaml:"systemPromptFile"`
	ModelPolicy      string   `yaml:"modelPolicy"`
	Tags             []string `yaml:"tags"`
}

type bindingCatalogFile struct {
	Bindings []bindingConfig `yaml:"bindings"`
}

type bindingConfig struct {
	ID               string                  `yaml:"id"`
	Tool             string                  `yaml:"tool"`
	Optional         bool                    `yaml:"optional"`
	Constants        map[string]any          `yaml:"constants"`
	ParamsTemplate   string                  `yaml:"paramsTemplate"`
	SuccessArtifacts []bindingArtifactConfig `yaml:"successArtifacts"`
	FailureArtifacts []bindingArtifactConfig `yaml:"failureArtifacts"`
}

type bindingArtifactConfig struct {
	Artifact        string `yaml:"artifact"`
	FromJSONPointer string `yaml:"fromJsonPointer"`
	Template        string `yaml:"template"`
	Value           any    `yaml:"value"`
}

type catalogToolManifest struct {
	Manifest       nodes.ToolManifest
	PromptTemplate string
	SystemPrompt   string
	ModelPolicy    string
}

type builtinProvider struct {
	log           *slog.Logger
	modelCfg      ModelToolConfig
	modelClient   JSONModelClient
	deterministic map[string]nodes.ToolDefinition
}

type httpProvider struct {
	cfg            providerConfig
	baseURL        string
	apiKey         string
	httpClient     *http.Client
	discovered     map[string]remoteProviderTool
	disabledReason string
}

type promptModelProvider struct {
	cfg            providerConfig
	expectedTool   string
	modelClient    JSONModelClient
	disabledReason string
}

type remoteProviderTool struct {
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Deterministic *bool           `json:"deterministic,omitempty"`
	Timeout       string          `json:"timeout,omitempty"`
	RetryBudget   *int            `json:"retryBudget,omitempty"`
	InputSchema   json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema  json.RawMessage `json:"outputSchema,omitempty"`
}

type remoteProviderListEnvelope struct {
	Items []remoteProviderTool `json:"items"`
}

type remoteInvokeRequest struct {
	CallID string          `json:"callId"`
	Params json.RawMessage `json:"params"`
}

type remoteInvokeResponse struct {
	Result json.RawMessage    `json:"result,omitempty"`
	Error  *remoteInvokeError `json:"error,omitempty"`
}

type remoteInvokeError struct {
	Code      string `json:"code,omitempty"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable,omitempty"`
}

const (
	builtinOpenAIJSONTool           = "openai.chat.completions.json"
	anthropicMessagesJSONTool       = "anthropic.messages.json"
	geminiGenerateContentJSONTool   = "gemini.generate_content.json"
	deepSeekChatCompletionsJSONTool = "deepseek.chat.completions.json"
)

func LoadCatalog(ctx context.Context, dir string, log *slog.Logger, modelCfg ModelToolConfig) (*LoadedCatalog, error) {
	providers, err := loadProviders(ctx, dir, log, modelCfg)
	if err != nil {
		return nil, err
	}
	manifests, err := loadToolManifests(dir)
	if err != nil {
		return nil, err
	}
	defs := make([]nodes.ToolDefinition, 0, len(manifests))
	toolNames := make(map[string]struct{}, len(manifests))
	for _, manifest := range manifests {
		provider, ok := providers[manifest.Manifest.Provider]
		if !ok {
			return nil, fmt.Errorf("tool %s references unknown provider %s", manifest.Manifest.Name, manifest.Manifest.Provider)
		}
		def, err := provider.Build(ctx, manifest)
		if err != nil {
			return nil, fmt.Errorf("build tool %s: %w", manifest.Manifest.Name, err)
		}
		defs = append(defs, def)
		toolNames[manifest.Manifest.Name] = struct{}{}
	}
	bindings, err := loadToolBindings(dir, toolNames)
	if err != nil {
		return nil, err
	}
	registry := nodes.NewStaticToolRegistry(defs...)
	bindingResolver := nodes.NewStaticToolBindingResolver(bindings...)
	return &LoadedCatalog{Registry: registry, Bindings: bindingResolver}, nil
}

func loadProviders(ctx context.Context, dir string, log *slog.Logger, modelCfg ModelToolConfig) (map[string]catalogProvider, error) {
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
			defs := make(map[string]nodes.ToolDefinition)
			for _, def := range LocalToolDefinitions() {
				defs[def.Manifest.Name] = def
			}
			providers[cfg.ID] = &builtinProvider{
				log:           log,
				modelCfg:      modelCfg,
				modelClient:   NewOpenAICompatibleJSONClient(modelCfg, log),
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
			provider, err := newPromptModelProvider(cfg, anthropicMessagesJSONTool, NewAnthropicMessagesJSONClient, "https://api.anthropic.com/v1/messages", log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		case "gemini":
			provider, err := newPromptModelProvider(cfg, geminiGenerateContentJSONTool, func(cfg ModelToolConfig, _ string, log *slog.Logger) JSONModelClient {
				return NewGeminiGenerateContentJSONClient(cfg, log)
			}, "https://generativelanguage.googleapis.com/v1beta", log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		case "openai":
			provider, err := newPromptModelProvider(cfg, builtinOpenAIJSONTool, func(cfg ModelToolConfig, _ string, log *slog.Logger) JSONModelClient {
				return NewOpenAICompatibleJSONClient(cfg, log)
			}, "https://api.openai.com/v1/chat/completions", log)
			if err != nil {
				return nil, err
			}
			providers[cfg.ID] = provider
		case "deepseek":
			provider, err := newPromptModelProvider(cfg, deepSeekChatCompletionsJSONTool, func(cfg ModelToolConfig, _ string, log *slog.Logger) JSONModelClient {
				return NewDeepSeekChatCompletionsJSONClient(cfg, log)
			}, "https://api.deepseek.com/chat/completions", log)
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

func loadToolManifests(dir string) ([]catalogToolManifest, error) {
	paths, err := yamlFilePaths(filepath.Join(dir, "manifests"))
	if err != nil {
		return nil, err
	}
	manifests := make([]catalogToolManifest, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read manifest %s: %w", path, err)
		}
		var cfg toolManifestConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse manifest %s: %w", path, err)
		}
		manifest, err := normalizeToolManifest(dir, cfg)
		if err != nil {
			return nil, fmt.Errorf("manifest %s: %w", path, err)
		}
		if _, exists := seen[manifest.Manifest.Name]; exists {
			return nil, fmt.Errorf("duplicate tool manifest name %s", manifest.Manifest.Name)
		}
		seen[manifest.Manifest.Name] = struct{}{}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

type modelClientFactory func(ModelToolConfig, string, *slog.Logger) JSONModelClient

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

func providerModelConfig(cfg providerConfig, defaultAPIURL string) (ModelToolConfig, string) {
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
	return ModelToolConfig{
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

func normalizeToolManifest(dir string, cfg toolManifestConfig) (catalogToolManifest, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return catalogToolManifest{}, fmt.Errorf("name required")
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		return catalogToolManifest{}, fmt.Errorf("provider required")
	}
	if strings.TrimSpace(cfg.ProviderToolName) == "" {
		return catalogToolManifest{}, fmt.Errorf("providerToolName required")
	}
	timeout, err := parseDurationString(cfg.Timeout)
	if err != nil {
		return catalogToolManifest{}, fmt.Errorf("invalid timeout: %w", err)
	}
	inputSchema, err := loadSchemaValue(dir, cfg.InputSchemaFile, cfg.InputSchema)
	if err != nil {
		return catalogToolManifest{}, fmt.Errorf("input schema: %w", err)
	}
	outputSchema, err := loadSchemaValue(dir, cfg.OutputSchemaFile, cfg.OutputSchema)
	if err != nil {
		return catalogToolManifest{}, fmt.Errorf("output schema: %w", err)
	}
	promptTemplate, err := loadOptionalTextFile(dir, cfg.PromptFile)
	if err != nil {
		return catalogToolManifest{}, fmt.Errorf("prompt file: %w", err)
	}
	systemPrompt, err := loadOptionalTextFile(dir, cfg.SystemPromptFile)
	if err != nil {
		return catalogToolManifest{}, fmt.Errorf("system prompt file: %w", err)
	}
	manifest := nodes.ToolManifest{
		Name:             strings.TrimSpace(cfg.Name),
		Provider:         strings.TrimSpace(cfg.Provider),
		ProviderToolName: strings.TrimSpace(cfg.ProviderToolName),
		Description:      strings.TrimSpace(cfg.Description),
		Timeout:          timeout,
		InputSchema:      inputSchema,
		OutputSchema:     outputSchema,
		Tags:             append([]string(nil), cfg.Tags...),
	}
	if cfg.Deterministic != nil {
		manifest.Deterministic = *cfg.Deterministic
	}
	if cfg.RetryBudget != nil {
		manifest.RetryBudget = *cfg.RetryBudget
	}
	return catalogToolManifest{
		Manifest:       manifest,
		PromptTemplate: promptTemplate,
		SystemPrompt:   systemPrompt,
		ModelPolicy:    strings.TrimSpace(cfg.ModelPolicy),
	}, nil
}

func loadToolBindings(dir string, toolNames map[string]struct{}) ([]nodes.ToolBinding, error) {
	paths, err := yamlFilePaths(filepath.Join(dir, "bindings"))
	if err != nil {
		return nil, err
	}
	bindings := make([]nodes.ToolBinding, 0)
	seen := make(map[string]struct{})
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read binding %s: %w", path, err)
		}
		fileBindings, err := decodeBindingFile(data)
		if err != nil {
			return nil, fmt.Errorf("parse binding %s: %w", path, err)
		}
		for _, cfg := range fileBindings {
			binding, err := normalizeBinding(cfg)
			if err != nil {
				return nil, fmt.Errorf("binding %s: %w", path, err)
			}
			if _, ok := toolNames[binding.ToolName]; !ok {
				return nil, fmt.Errorf("binding %s references unknown tool %s", binding.ID, binding.ToolName)
			}
			if _, exists := seen[binding.ID]; exists {
				return nil, fmt.Errorf("duplicate binding id %s", binding.ID)
			}
			seen[binding.ID] = struct{}{}
			bindings = append(bindings, binding)
		}
	}
	return bindings, nil
}

func decodeBindingFile(data []byte) ([]bindingConfig, error) {
	var wrapper bindingCatalogFile
	if err := yaml.Unmarshal(data, &wrapper); err == nil && wrapper.Bindings != nil {
		return wrapper.Bindings, nil
	}
	var single bindingConfig
	if err := yaml.Unmarshal(data, &single); err != nil {
		return nil, err
	}
	if strings.TrimSpace(single.ID) == "" {
		return nil, fmt.Errorf("binding id required")
	}
	return []bindingConfig{single}, nil
}

func normalizeBinding(cfg bindingConfig) (nodes.ToolBinding, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return nodes.ToolBinding{}, fmt.Errorf("id required")
	}
	if strings.TrimSpace(cfg.Tool) == "" {
		return nodes.ToolBinding{}, fmt.Errorf("tool required")
	}
	binding := nodes.ToolBinding{
		ID:             strings.TrimSpace(cfg.ID),
		ToolName:       strings.TrimSpace(cfg.Tool),
		Optional:       cfg.Optional,
		Constants:      cfg.Constants,
		ParamsTemplate: cfg.ParamsTemplate,
	}
	var err error
	binding.SuccessArtifacts, err = normalizeBindingMappings(cfg.SuccessArtifacts)
	if err != nil {
		return nodes.ToolBinding{}, fmt.Errorf("successArtifacts: %w", err)
	}
	binding.FailureArtifacts, err = normalizeBindingMappings(cfg.FailureArtifacts)
	if err != nil {
		return nodes.ToolBinding{}, fmt.Errorf("failureArtifacts: %w", err)
	}
	return binding, nil
}

func normalizeBindingMappings(configs []bindingArtifactConfig) ([]nodes.ToolArtifactBinding, error) {
	mappings := make([]nodes.ToolArtifactBinding, 0, len(configs))
	for _, cfg := range configs {
		mapping := nodes.ToolArtifactBinding{
			Artifact:        strings.TrimSpace(cfg.Artifact),
			FromJSONPointer: strings.TrimSpace(cfg.FromJSONPointer),
			Template:        cfg.Template,
		}
		if cfg.Value != nil {
			literal, err := literalArtifactValue(cfg.Value)
			if err != nil {
				return nil, err
			}
			mapping.Value = &literal
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

func literalArtifactValue(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	return catalogArtifactString(decoded)
}

func parseDurationString(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}

func loadSchemaValue(baseDir, file string, inline any) (json.RawMessage, error) {
	if strings.TrimSpace(file) != "" {
		data, err := os.ReadFile(filepath.Join(baseDir, file))
		if err != nil {
			return nil, err
		}
		trimmed := bytes.TrimSpace(data)
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("schema file empty")
		}
		if !json.Valid(trimmed) {
			var yamlValue any
			if err := yaml.Unmarshal(trimmed, &yamlValue); err != nil {
				return nil, fmt.Errorf("schema file must contain json or yaml: %w", err)
			}
			trimmed, err = json.Marshal(yamlValue)
			if err != nil {
				return nil, err
			}
		}
		if _, err := compileSchemaValidator(trimmed); err != nil {
			return nil, err
		}
		return json.RawMessage(trimmed), nil
	}
	if inline == nil {
		return nil, nil
	}
	raw, err := json.Marshal(inline)
	if err != nil {
		return nil, err
	}
	if _, err := compileSchemaValidator(raw); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func loadOptionalTextFile(baseDir, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(filepath.Join(baseDir, path))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func yamlFilePaths(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

func (p *builtinProvider) Build(_ context.Context, manifest catalogToolManifest) (nodes.ToolDefinition, error) {
	if def, ok := p.deterministic[manifest.Manifest.ProviderToolName]; ok {
		return p.wrapProviderDefinition(manifest, def)
	}
	if manifest.Manifest.ProviderToolName == builtinOpenAIJSONTool {
		return p.buildGenericOpenAIJSONTool(manifest)
	}
	return nodes.ToolDefinition{}, fmt.Errorf("unsupported builtin provider tool %s", manifest.Manifest.ProviderToolName)
}

func (p *builtinProvider) wrapProviderDefinition(manifest catalogToolManifest, def nodes.ToolDefinition) (nodes.ToolDefinition, error) {
	providerManifest := def.Manifest
	merged := manifest.Manifest
	if merged.Description == "" {
		merged.Description = providerManifest.Description
	}
	if merged.Timeout == 0 {
		merged.Timeout = providerManifest.Timeout
	}
	if merged.RetryBudget == 0 {
		merged.RetryBudget = providerManifest.RetryBudget
	}
	if len(merged.InputSchema) == 0 {
		merged.InputSchema = providerManifest.InputSchema
	} else if len(providerManifest.InputSchema) > 0 && !jsonSchemaEquivalent(merged.InputSchema, providerManifest.InputSchema) {
		return nodes.ToolDefinition{}, fmt.Errorf("input schema mismatch with provider tool %s", manifest.Manifest.ProviderToolName)
	}
	if len(merged.OutputSchema) == 0 {
		merged.OutputSchema = providerManifest.OutputSchema
	} else if len(providerManifest.OutputSchema) > 0 && !jsonSchemaEquivalent(merged.OutputSchema, providerManifest.OutputSchema) {
		return nodes.ToolDefinition{}, fmt.Errorf("output schema mismatch with provider tool %s", manifest.Manifest.ProviderToolName)
	}
	def.Manifest = merged
	return def, nil
}

func (p *builtinProvider) buildGenericOpenAIJSONTool(manifest catalogToolManifest) (nodes.ToolDefinition, error) {
	return buildPromptModelToolDefinition(manifest, builtinOpenAIJSONTool, p.modelClient, "")
}

func (p *promptModelProvider) Build(_ context.Context, manifest catalogToolManifest) (nodes.ToolDefinition, error) {
	return buildPromptModelToolDefinition(manifest, p.expectedTool, p.modelClient, p.disabledReason)
}

func buildPromptModelToolDefinition(manifest catalogToolManifest, expectedTool string, client JSONModelClient, disabledReason string) (nodes.ToolDefinition, error) {
	if manifest.Manifest.ProviderToolName != expectedTool {
		return nodes.ToolDefinition{}, fmt.Errorf("unsupported prompt provider tool %s", manifest.Manifest.ProviderToolName)
	}
	if strings.TrimSpace(manifest.PromptTemplate) == "" {
		return nodes.ToolDefinition{}, fmt.Errorf("prompt template required for %s", expectedTool)
	}
	validateParams, err := compileSchemaValidator(manifest.Manifest.InputSchema)
	if err != nil {
		return nodes.ToolDefinition{}, err
	}
	validateResult, err := compileSchemaValidator(manifest.Manifest.OutputSchema)
	if err != nil {
		return nodes.ToolDefinition{}, err
	}
	return nodes.ToolDefinition{
		Manifest:       manifest.Manifest,
		ValidateParams: validateParams,
		ValidateResult: validateResult,
		Handler: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			if client == nil {
				reason := strings.TrimSpace(disabledReason)
				if reason == "" {
					reason = "model client not configured"
				}
				return nil, fmt.Errorf("%w: %s", nodes.ErrToolDisabled, reason)
			}
			prompt, systemPrompt, err := renderPromptTemplates(manifest, params)
			if err != nil {
				return nil, fmt.Errorf("render prompt: %w", err)
			}
			request := JSONModelRequest{
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

func (p *httpProvider) Build(ctx context.Context, manifest catalogToolManifest) (nodes.ToolDefinition, error) {
	if p.disabledReason != "" {
		return disabledToolDefinition(manifest.Manifest, fmt.Sprintf("provider %s %s", p.cfg.ID, p.disabledReason))
	}
	remoteTools, err := p.discovery(ctx)
	if err != nil {
		return nodes.ToolDefinition{}, err
	}
	remote, ok := remoteTools[manifest.Manifest.ProviderToolName]
	if !ok {
		return nodes.ToolDefinition{}, fmt.Errorf("remote tool %s not found", manifest.Manifest.ProviderToolName)
	}
	merged := manifest.Manifest
	if merged.Description == "" {
		merged.Description = remote.Description
	}
	if remote.Deterministic != nil && !merged.Deterministic {
		merged.Deterministic = *remote.Deterministic
	}
	if merged.Timeout == 0 && strings.TrimSpace(remote.Timeout) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(remote.Timeout))
		if err != nil {
			return nodes.ToolDefinition{}, fmt.Errorf("remote timeout: %w", err)
		}
		merged.Timeout = parsed
	}
	if merged.RetryBudget == 0 && remote.RetryBudget != nil {
		merged.RetryBudget = *remote.RetryBudget
	}
	if len(merged.InputSchema) == 0 {
		merged.InputSchema = remote.InputSchema
	} else if len(remote.InputSchema) > 0 && !jsonSchemaEquivalent(merged.InputSchema, remote.InputSchema) {
		return nodes.ToolDefinition{}, fmt.Errorf("input schema mismatch with remote tool %s", remote.Name)
	}
	if len(merged.OutputSchema) == 0 {
		merged.OutputSchema = remote.OutputSchema
	} else if len(remote.OutputSchema) > 0 && !jsonSchemaEquivalent(merged.OutputSchema, remote.OutputSchema) {
		return nodes.ToolDefinition{}, fmt.Errorf("output schema mismatch with remote tool %s", remote.Name)
	}
	validateParams, err := compileSchemaValidator(merged.InputSchema)
	if err != nil {
		return nodes.ToolDefinition{}, err
	}
	validateResult, err := compileSchemaValidator(merged.OutputSchema)
	if err != nil {
		return nodes.ToolDefinition{}, err
	}
	return nodes.ToolDefinition{
		Manifest:       merged,
		ValidateParams: validateParams,
		ValidateResult: validateResult,
		Handler: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			return p.invoke(ctx, merged.ProviderToolName, params)
		},
	}, nil
}

func disabledToolDefinition(manifest nodes.ToolManifest, reason string) (nodes.ToolDefinition, error) {
	validateParams, err := compileSchemaValidator(manifest.InputSchema)
	if err != nil {
		return nodes.ToolDefinition{}, err
	}
	validateResult, err := compileSchemaValidator(manifest.OutputSchema)
	if err != nil {
		return nodes.ToolDefinition{}, err
	}
	if strings.TrimSpace(reason) == "" {
		reason = "provider unavailable"
	}
	return nodes.ToolDefinition{
		Manifest:       manifest,
		ValidateParams: validateParams,
		ValidateResult: validateResult,
		Handler: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: %s", nodes.ErrToolDisabled, reason)
		},
	}, nil
}

func (p *httpProvider) discovery(ctx context.Context) (map[string]remoteProviderTool, error) {
	if p.discovered != nil {
		return p.discovered, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/v1/tools", nil)
	if err != nil {
		return nil, err
	}
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var tools []remoteProviderTool
	if err := json.Unmarshal(body, &tools); err != nil {
		var envelope remoteProviderListEnvelope
		if errEnvelope := json.Unmarshal(body, &envelope); errEnvelope != nil {
			return nil, fmt.Errorf("decode tool list: %w", err)
		}
		tools = envelope.Items
	}
	p.discovered = make(map[string]remoteProviderTool, len(tools))
	for _, tool := range tools {
		p.discovered[tool.Name] = tool
	}
	return p.discovered, nil
}

func (p *httpProvider) invoke(ctx context.Context, remoteToolName string, params json.RawMessage) (json.RawMessage, error) {
	endpoint := p.baseURL + "/v1/tools/" + url.PathEscape(remoteToolName) + ":invoke"
	body, err := json.Marshal(remoteInvokeRequest{
		CallID: fmt.Sprintf("call-%d", time.Now().UnixNano()),
		Params: append(json.RawMessage(nil), params...),
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nodes.MarkToolRetryable(fmt.Errorf("remote provider transport: %w", err))
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, nodes.MarkToolRetryable(fmt.Errorf("remote provider read: %w", err))
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		return nil, nodes.MarkToolRetryable(fmt.Errorf("remote provider status %d: %s", response.StatusCode, compactProviderError(responseBody)))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("remote provider status %d: %s", response.StatusCode, compactProviderError(responseBody))
	}
	var decoded remoteInvokeResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode invoke response: %w", err)
	}
	if decoded.Error != nil {
		wrapped := fmt.Errorf("remote provider error %s: %s", strings.TrimSpace(decoded.Error.Code), strings.TrimSpace(decoded.Error.Message))
		if decoded.Error.Retryable {
			return nil, nodes.MarkToolRetryable(wrapped)
		}
		return nil, wrapped
	}
	if len(decoded.Result) == 0 {
		return nil, fmt.Errorf("remote provider returned empty result")
	}
	return decoded.Result, nil
}

func jsonSchemaEquivalent(left, right json.RawMessage) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == len(right)
	}
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	leftJSON, err := json.Marshal(leftValue)
	if err != nil {
		return false
	}
	rightJSON, err := json.Marshal(rightValue)
	if err != nil {
		return false
	}
	return bytes.Equal(leftJSON, rightJSON)
}

func catalogArtifactString(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", fmt.Errorf("value is null")
	case string:
		return typed, nil
	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed)), nil
		}
		return fmt.Sprintf("%v", typed), nil
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
}
