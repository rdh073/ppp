package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ToolManifest describes a tool's identity, routing metadata, and parameter schema.
type ToolManifest struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Provider         string          `json:"provider,omitempty"`
	ProviderToolName string          `json:"providerToolName,omitempty"`
	Deterministic    bool            `json:"deterministic,omitempty"`
	Timeout          time.Duration   `json:"timeout,omitempty"`
	RetryBudget      int             `json:"retryBudget,omitempty"`
	InputSchema      json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema     json.RawMessage `json:"outputSchema,omitempty"`
	Tags             []string        `json:"tags,omitempty"`
}

// ToolDefinition pairs a manifest with its validation hooks and handler function.
type ToolDefinition struct {
	Manifest       ToolManifest
	ValidateParams func(json.RawMessage) error
	ValidateResult func(json.RawMessage) error
	Handler        func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)
}

// ToolRegistry exposes tool manifests and invocation.
type ToolRegistry interface {
	Manifest(toolName string) (ToolManifest, bool)
	Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error)
}

// ToolArtifactBinding maps a tool output field to a workflow artifact.
type ToolArtifactBinding struct {
	Artifact        string  `json:"artifact"`
	FromJSONPointer string  `json:"fromJsonPointer,omitempty"`
	Template        string  `json:"template,omitempty"`
	Value           *string `json:"value,omitempty"`
}

// ToolBinding maps a workflow binding ID to a tool call with artifact routing.
type ToolBinding struct {
	ID               string                `json:"id"`
	ToolName         string                `json:"toolName"`
	Optional         bool                  `json:"optional,omitempty"`
	Constants        map[string]any        `json:"constants,omitempty"`
	ParamsTemplate   string                `json:"paramsTemplate,omitempty"`
	SuccessArtifacts []ToolArtifactBinding `json:"successArtifacts,omitempty"`
	FailureArtifacts []ToolArtifactBinding `json:"failureArtifacts,omitempty"`
}

// ToolBindingResolver resolves a binding ID to its ToolBinding.
type ToolBindingResolver interface {
	Binding(bindingID string) (ToolBinding, bool)
}

// Sentinel errors.
var (
	ErrToolUnsupported   = errors.New("tool unsupported")
	ErrToolDisabled      = errors.New("tool disabled")
	ErrToolInvalidParams = errors.New("tool invalid params")
	ErrToolRetryable     = errors.New("tool retryable")
)

// retryableToolError wraps an error to signal the caller may retry.
type retryableToolError struct{ cause error }

func (e retryableToolError) Error() string    { return e.cause.Error() }
func (e retryableToolError) Unwrap() error    { return e.cause }
func (e retryableToolError) Is(target error) bool {
	return target == ErrToolRetryable
}

// MarkToolRetryable wraps err so callers can detect it as retryable.
func MarkToolRetryable(err error) error { return retryableToolError{cause: err} }

// IsToolRetryable reports whether err is a retryable tool error.
func IsToolRetryable(err error) bool {
	return errors.Is(err, ErrToolRetryable)
}

// StaticToolRegistry implements ToolRegistry from a fixed list of ToolDefinitions.
type StaticToolRegistry struct {
	defs map[string]ToolDefinition
}

// NewStaticToolRegistry builds a StaticToolRegistry from the given definitions.
func NewStaticToolRegistry(defs ...ToolDefinition) StaticToolRegistry {
	m := make(map[string]ToolDefinition, len(defs))
	for _, d := range defs {
		m[d.Manifest.Name] = d
	}
	return StaticToolRegistry{defs: m}
}

func (r StaticToolRegistry) Manifest(toolName string) (ToolManifest, bool) {
	d, ok := r.defs[toolName]
	return d.Manifest, ok
}

func (r StaticToolRegistry) Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error) {
	d, ok := r.defs[toolName]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolUnsupported, toolName)
	}
	if d.ValidateParams != nil {
		if err := d.ValidateParams(params); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrToolInvalidParams, err)
		}
	}
	budget := d.Manifest.RetryBudget
	var result json.RawMessage
	var err error
	for attempt := 0; attempt <= budget; attempt++ {
		result, err = d.Handler(ctx, params)
		if err == nil {
			break
		}
		if !IsToolRetryable(err) || attempt == budget {
			return nil, err
		}
	}
	if d.ValidateResult != nil {
		if err := d.ValidateResult(result); err != nil {
			return nil, fmt.Errorf("tool %s result validation: %w", toolName, err)
		}
	}
	return result, nil
}

// staticToolBindingResolver implements ToolBindingResolver from a fixed list.
type staticToolBindingResolver struct {
	bindings map[string]ToolBinding
}

// NewStaticToolBindingResolver builds a ToolBindingResolver from the given bindings.
func NewStaticToolBindingResolver(bindings ...ToolBinding) ToolBindingResolver {
	m := make(map[string]ToolBinding, len(bindings))
	for _, b := range bindings {
		m[b.ID] = b
	}
	return &staticToolBindingResolver{bindings: m}
}

func (r *staticToolBindingResolver) Binding(bindingID string) (ToolBinding, bool) {
	b, ok := r.bindings[bindingID]
	return b, ok
}
