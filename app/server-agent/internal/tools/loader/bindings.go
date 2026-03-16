package loader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/autosdk/ppp/server-agent/internal/tools"
)

func loadToolBindings(dir string, toolNames map[string]struct{}) ([]tools.ToolBinding, error) {
	paths, err := yamlFilePaths(filepath.Join(dir, "bindings"))
	if err != nil {
		return nil, err
	}
	bindings := make([]tools.ToolBinding, 0)
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

func normalizeBinding(cfg bindingConfig) (tools.ToolBinding, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return tools.ToolBinding{}, fmt.Errorf("id required")
	}
	if strings.TrimSpace(cfg.Tool) == "" {
		return tools.ToolBinding{}, fmt.Errorf("tool required")
	}
	binding := tools.ToolBinding{
		ID:             strings.TrimSpace(cfg.ID),
		ToolName:       strings.TrimSpace(cfg.Tool),
		Optional:       cfg.Optional,
		Constants:      cfg.Constants,
		ParamsTemplate: cfg.ParamsTemplate,
	}
	var err error
	binding.SuccessArtifacts, err = normalizeBindingMappings(cfg.SuccessArtifacts)
	if err != nil {
		return tools.ToolBinding{}, fmt.Errorf("successArtifacts: %w", err)
	}
	binding.FailureArtifacts, err = normalizeBindingMappings(cfg.FailureArtifacts)
	if err != nil {
		return tools.ToolBinding{}, fmt.Errorf("failureArtifacts: %w", err)
	}
	return binding, nil
}

func normalizeBindingMappings(configs []bindingArtifactConfig) ([]tools.ToolArtifactBinding, error) {
	mappings := make([]tools.ToolArtifactBinding, 0, len(configs))
	for _, cfg := range configs {
		mapping := tools.ToolArtifactBinding{
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
