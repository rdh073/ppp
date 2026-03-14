package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

type compositeToolRegistry struct {
	registries []nodes.ToolRegistry
}

// NewCompositeToolRegistry chains registries in priority order. The first
// registry that exposes a manifest for a tool owns invocation for that tool.
func NewCompositeToolRegistry(registries ...nodes.ToolRegistry) nodes.ToolRegistry {
	filtered := make([]nodes.ToolRegistry, 0, len(registries))
	for _, registry := range registries {
		if registry != nil {
			filtered = append(filtered, registry)
		}
	}
	return compositeToolRegistry{registries: filtered}
}

func (r compositeToolRegistry) Manifest(toolName string) (nodes.ToolManifest, bool) {
	for _, registry := range r.registries {
		if manifest, ok := registry.Manifest(toolName); ok {
			return manifest, true
		}
	}
	return nodes.ToolManifest{}, false
}

func (r compositeToolRegistry) Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error) {
	for _, registry := range r.registries {
		if _, ok := registry.Manifest(toolName); ok {
			return registry.Invoke(ctx, toolName, params)
		}
	}
	return nil, fmt.Errorf("%w: %s", nodes.ErrToolUnsupported, toolName)
}
