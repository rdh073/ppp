package loader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/autosdk/ppp/server-agent/internal/tools"
)

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

func normalizeToolManifest(dir string, cfg toolManifestConfig) (catalogToolManifest, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return catalogToolManifest{}, fmt.Errorf("name required")
	}
	isChain := len(cfg.Providers) > 0
	if !isChain {
		if strings.TrimSpace(cfg.Provider) == "" {
			return catalogToolManifest{}, fmt.Errorf("provider required")
		}
		if strings.TrimSpace(cfg.ProviderToolName) == "" {
			return catalogToolManifest{}, fmt.Errorf("providerToolName required")
		}
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
	provider := strings.TrimSpace(cfg.Provider)
	if isChain && provider == "" {
		provider = "chain"
	}
	manifest := tools.ToolManifest{
		Name:             strings.TrimSpace(cfg.Name),
		Provider:         provider,
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

	var providerChain []catalogChainEntry
	if isChain {
		providerChain = make([]catalogChainEntry, len(cfg.Providers))
		for i, entry := range cfg.Providers {
			if strings.TrimSpace(entry.Provider) == "" {
				return catalogToolManifest{}, fmt.Errorf("chain entry %d: provider required", i)
			}
			if strings.TrimSpace(entry.ProviderToolName) == "" {
				return catalogToolManifest{}, fmt.Errorf("chain entry %d: providerToolName required", i)
			}
			providerChain[i] = catalogChainEntry{
				ProviderID:       strings.TrimSpace(entry.Provider),
				ProviderToolName: strings.TrimSpace(entry.ProviderToolName),
			}
		}
	}

	return catalogToolManifest{
		Manifest:       manifest,
		PromptTemplate: promptTemplate,
		SystemPrompt:   systemPrompt,
		ModelPolicy:    strings.TrimSpace(cfg.ModelPolicy),
		ProviderChain:  providerChain,
		ChainFallback:  strings.TrimSpace(cfg.Fallback),
	}, nil
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
		if _, err := tools.CompileSchemaValidator(trimmed); err != nil {
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
	if _, err := tools.CompileSchemaValidator(raw); err != nil {
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
