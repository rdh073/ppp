package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

var (
	// ErrToolUnsupported indicates the workflow referenced a tool that is not
	// available in the current registry.
	ErrToolUnsupported = errors.New("tool unsupported")
	// ErrToolDisabled indicates the tool exists conceptually but has no active
	// production implementation yet.
	ErrToolDisabled = errors.New("tool disabled")
	// ErrToolInvalidParams indicates the caller supplied params that violate the
	// manifest contract for the selected tool.
	ErrToolInvalidParams = errors.New("tool invalid params")
	// ErrToolInvalidResult indicates the tool handler returned a result that does
	// not satisfy the declared output contract.
	ErrToolInvalidResult = errors.New("tool invalid result")
)

// ToolManifest describes the runtime contract of a server-side workflow tool.
// Phase 1 uses the manifest for lookup and timeout enforcement; schema
// validation can be layered in later without changing the node contract.
type ToolManifest struct {
	Name          string
	Description   string
	Deterministic bool
	Timeout       time.Duration
	InputSchema   json.RawMessage
	OutputSchema  json.RawMessage
}

// ToolFunc is the execution function for a manifest-backed tool.
type ToolFunc func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)

// ToolDefinition binds a manifest to its implementation.
type ToolDefinition struct {
	Manifest       ToolManifest
	Handler        ToolFunc
	ValidateParams func(json.RawMessage) error
	ValidateResult func(json.RawMessage) error
}

// ToolRegistry is the dependency surface for external tool invocations.
// Implementations may call LLMs, REST APIs, or local functions.
type ToolRegistry interface {
	// Manifest returns the contract metadata for the named tool.
	Manifest(toolName string) (ToolManifest, bool)
	// Invoke calls the named tool with the given JSON params and returns a JSON result.
	Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error)
}

// DisabledToolRegistry fails closed for all tool invocations. This is the
// production-safe default until real tools are registered.
type DisabledToolRegistry struct{}

func (DisabledToolRegistry) Manifest(string) (ToolManifest, bool) {
	return ToolManifest{}, false
}

func (DisabledToolRegistry) Invoke(_ context.Context, toolName string, _ json.RawMessage) (json.RawMessage, error) {
	return nil, fmt.Errorf("%w: %s", ErrToolUnsupported, toolName)
}

// StaticToolRegistry is a manifest-backed in-process registry for local tools
// and tests.
type StaticToolRegistry struct {
	defs map[string]ToolDefinition
}

func NewStaticToolRegistry(defs ...ToolDefinition) StaticToolRegistry {
	byName := make(map[string]ToolDefinition, len(defs))
	for _, def := range defs {
		if def.Manifest.Name == "" {
			panic("nodes.NewStaticToolRegistry: tool manifest name required")
		}
		if _, exists := byName[def.Manifest.Name]; exists {
			panic("nodes.NewStaticToolRegistry: duplicate tool manifest name: " + def.Manifest.Name)
		}
		byName[def.Manifest.Name] = def
	}
	return StaticToolRegistry{defs: byName}
}

func (r StaticToolRegistry) Manifest(toolName string) (ToolManifest, bool) {
	def, ok := r.defs[toolName]
	if !ok {
		return ToolManifest{}, false
	}
	return def.Manifest, true
}

func (r StaticToolRegistry) Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error) {
	def, ok := r.defs[toolName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolUnsupported, toolName)
	}
	if def.Handler == nil {
		return nil, fmt.Errorf("%w: %s", ErrToolDisabled, toolName)
	}
	if def.ValidateParams != nil {
		if err := def.ValidateParams(params); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrToolInvalidParams, toolName, err)
		}
	}
	result, err := def.Handler(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 || !json.Valid(result) {
		return nil, fmt.Errorf("%w: %s: result must be valid JSON", ErrToolInvalidResult, toolName)
	}
	if def.ValidateResult != nil {
		if err := def.ValidateResult(result); err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrToolInvalidResult, toolName, err)
		}
	}
	return result, nil
}

// ToolCallNode reads the pending_tool and pending_tool_params artifacts, invokes
// the ToolRegistry, stores the result as tool_result.
// Routing (->Decide on success, ->Resync on failure) is handled by the Runner via the def.
type ToolCallNode struct {
	tools ToolRegistry
}

func NewToolCallNode(tools ToolRegistry) *ToolCallNode {
	return &ToolCallNode{tools: tools}
}

func (n *ToolCallNode) Run(ctx context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	toolName := input.State.Artifacts["pending_tool"]
	if toolName == "" {
		// Nothing queued - report success; def will route to Decide.
		return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
	}

	rawParams := json.RawMessage(input.State.Artifacts["pending_tool_params"])
	if len(rawParams) == 0 {
		rawParams = json.RawMessage(`{}`)
	}

	manifest, ok := n.tools.Manifest(toolName)
	if !ok {
		return toolFailure(toolName, fmt.Errorf("%w: %s", ErrToolUnsupported, toolName)), nil
	}

	invokeCtx := ctx
	if manifest.Timeout > 0 {
		var cancel context.CancelFunc
		invokeCtx, cancel = context.WithTimeout(ctx, manifest.Timeout)
		defer cancel()
	}

	result, err := n.tools.Invoke(invokeCtx, toolName, rawParams)
	if err != nil {
		return toolFailure(toolName, err), nil
	}

	zero := intPtr(0)
	return workflow.NodeOutput{
		Status: workflow.NodeStatusSuccess,
		Artifacts: map[string]string{
			"tool_result":    string(result),
			"last_tool_name": toolName,
		},
		DeleteArtifacts: []string{"pending_tool", "pending_tool_params"},
		SetErrorCount:   zero,
	}, nil
}

func toolFailure(toolName string, err error) workflow.NodeOutput {
	return workflow.NodeOutput{
		Status: workflow.NodeStatusFailure,
		Artifacts: map[string]string{
			"resync_reason": fmt.Sprintf("toolcall %s failed: %v", toolName, err),
		},
		DeleteArtifacts: []string{"pending_tool", "pending_tool_params"},
	}
}

func intPtr(n int) *int { v := n; return &v }
