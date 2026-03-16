package loader

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

const builtinOpenAIJSONTool = "openai.chat.completions.json"

type builtinProvider struct {
	log           *slog.Logger
	modelCfg      llm.ModelToolConfig
	modelClient   llm.JSONModelClient
	deterministic map[string]tools.ToolDefinition
}

func (p *builtinProvider) Build(_ context.Context, manifest catalogToolManifest) (tools.ToolDefinition, error) {
	if def, ok := p.deterministic[manifest.Manifest.ProviderToolName]; ok {
		return p.wrapProviderDefinition(manifest, def)
	}
	if manifest.Manifest.ProviderToolName == builtinOpenAIJSONTool {
		return p.buildGenericOpenAIJSONTool(manifest)
	}
	return tools.ToolDefinition{}, fmt.Errorf("unsupported builtin provider tool %s", manifest.Manifest.ProviderToolName)
}

func (p *builtinProvider) wrapProviderDefinition(manifest catalogToolManifest, def tools.ToolDefinition) (tools.ToolDefinition, error) {
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
		return tools.ToolDefinition{}, fmt.Errorf("input schema mismatch with provider tool %s", manifest.Manifest.ProviderToolName)
	}
	if len(merged.OutputSchema) == 0 {
		merged.OutputSchema = providerManifest.OutputSchema
	} else if len(providerManifest.OutputSchema) > 0 && !jsonSchemaEquivalent(merged.OutputSchema, providerManifest.OutputSchema) {
		return tools.ToolDefinition{}, fmt.Errorf("output schema mismatch with provider tool %s", manifest.Manifest.ProviderToolName)
	}
	def.Manifest = merged
	return def, nil
}

func (p *builtinProvider) buildGenericOpenAIJSONTool(manifest catalogToolManifest) (tools.ToolDefinition, error) {
	return buildPromptModelToolDefinition(manifest, builtinOpenAIJSONTool, p.modelClient, "")
}
