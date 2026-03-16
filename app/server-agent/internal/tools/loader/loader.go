// Package loader reads a tool catalog directory (YAML providers, manifests,
// bindings) and wires it into a LoadedCatalog ready for use by the workflow
// engine.
package loader

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

// ModelToolConfig is re-exported from llm so callers (main.go) can do
// toolcatalog.ModelToolConfig without importing llm directly.
type ModelToolConfig = llm.ModelToolConfig

// LoadedCatalog is the result of loading a tool catalog directory.
type LoadedCatalog struct {
	Registry tools.ToolRegistry
	Bindings tools.ToolBindingResolver
}

// catalogProvider is the loader-internal dispatch interface: one per provider
// entry in providers.yaml. Unexported — no external consumer needs this.
type catalogProvider interface {
	Build(ctx context.Context, manifest catalogToolManifest) (tools.ToolDefinition, error)
}

// LoadCatalog reads providers.yaml, manifests/, bindings/ from dir and returns
// a wired LoadedCatalog. Fails closed if any file is invalid.
func LoadCatalog(ctx context.Context, dir string, log *slog.Logger, modelCfg ModelToolConfig) (*LoadedCatalog, error) {
	providers, err := loadProviders(ctx, dir, log, modelCfg)
	if err != nil {
		return nil, err
	}
	manifests, err := loadToolManifests(dir)
	if err != nil {
		return nil, err
	}
	defs := make([]tools.ToolDefinition, 0, len(manifests))
	toolNames := make(map[string]struct{}, len(manifests))
	for _, manifest := range manifests {
		var def tools.ToolDefinition
		if len(manifest.ProviderChain) > 0 {
			memberDefs := make([]tools.ToolDefinition, 0, len(manifest.ProviderChain))
			for _, entry := range manifest.ProviderChain {
				provider, ok := providers[entry.ProviderID]
				if !ok {
					return nil, fmt.Errorf("tool %s chain member references unknown provider %s", manifest.Manifest.Name, entry.ProviderID)
				}
				memberManifest := manifest
				memberManifest.Manifest.ProviderToolName = entry.ProviderToolName
				memberManifest.ProviderChain = nil
				memberDef, err := provider.Build(ctx, memberManifest)
				if err != nil {
					return nil, fmt.Errorf("build tool %s chain member %s: %w", manifest.Manifest.Name, entry.ProviderID, err)
				}
				memberDefs = append(memberDefs, memberDef)
			}
			def = buildChainToolDefinition(manifest.Manifest, memberDefs, manifest.ChainFallback)
		} else {
			provider, ok := providers[manifest.Manifest.Provider]
			if !ok {
				return nil, fmt.Errorf("tool %s references unknown provider %s", manifest.Manifest.Name, manifest.Manifest.Provider)
			}
			var err error
			def, err = provider.Build(ctx, manifest)
			if err != nil {
				return nil, fmt.Errorf("build tool %s: %w", manifest.Manifest.Name, err)
			}
		}
		defs = append(defs, def)
		toolNames[manifest.Manifest.Name] = struct{}{}
	}
	bindings, err := loadToolBindings(dir, toolNames)
	if err != nil {
		return nil, err
	}
	registry := tools.NewStaticToolRegistry(defs...)
	bindingResolver := tools.NewStaticToolBindingResolver(bindings...)
	return &LoadedCatalog{Registry: registry, Bindings: bindingResolver}, nil
}
