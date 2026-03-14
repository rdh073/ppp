package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

var (
	// ErrToolUnsupported indicates the workflow referenced a tool that is not
	// available in the current registry.
	ErrToolUnsupported = errors.New("tool unsupported")
	// ErrToolDisabled indicates the tool exists conceptually but has no active
	// production implementation yet.
	ErrToolDisabled = errors.New("tool disabled")
	// ErrToolRetryable marks transient tool failures that may be retried within
	// the tool's retry budget.
	ErrToolRetryable = errors.New("tool retryable")
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
	RetryBudget   int
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

type retryableToolError struct {
	err error
}

func (e retryableToolError) Error() string {
	return e.err.Error()
}

func (e retryableToolError) Unwrap() error {
	return e.err
}

func (e retryableToolError) Is(target error) bool {
	return target == ErrToolRetryable || errors.Is(e.err, target)
}

// MarkToolRetryable wraps an error so the registry can consume retry budget for
// transient failures while preserving the original cause for logs and tests.
func MarkToolRetryable(err error) error {
	if err == nil {
		return nil
	}
	return retryableToolError{err: err}
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

	retryBudget := def.Manifest.RetryBudget
	if retryBudget < 0 {
		retryBudget = 0
	}

	for attempt := 0; attempt <= retryBudget; attempt++ {
		result, err := def.Handler(ctx, params)
		if err != nil {
			if attempt < retryBudget && errors.Is(err, ErrToolRetryable) && ctx.Err() == nil {
				continue
			}
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

	return nil, fmt.Errorf("%w: %s: retry budget exhausted", ErrToolRetryable, toolName)
}

// ToolCallNode reads the pending_tool and pending_tool_params artifacts, invokes
// the ToolRegistry, stores the result as tool_result.
// Routing (->Decide on success, ->Resync on failure) is handled by the Runner via the def.
type ToolCallNode struct {
	tools   ToolRegistry
	metrics *telemetry.Registry
}

func NewToolCallNode(tools ToolRegistry, metrics ...*telemetry.Registry) *ToolCallNode {
	var registry *telemetry.Registry
	if len(metrics) > 0 {
		registry = metrics[0]
	}
	return &ToolCallNode{tools: tools, metrics: registry}
}

func (n *ToolCallNode) Run(ctx context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	toolName := input.State.Artifacts["pending_tool"]
	if toolName == "" {
		// Nothing queued - report success; def will route to Decide.
		return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
	}
	startedAt := time.Now()
	outcome := telemetry.ToolCallOutcomeFailure
	defer func() {
		if n.metrics == nil {
			return
		}
		n.metrics.ObserveToolCall(toolName, outcome, time.Since(startedAt))
	}()
	optional := input.State.Artifacts["pending_tool_optional"] == "true"

	rawParams := json.RawMessage(input.State.Artifacts["pending_tool_params"])
	if len(rawParams) == 0 {
		rawParams = json.RawMessage(`{}`)
	}

	manifest, ok := n.tools.Manifest(toolName)
	if !ok {
		outcome = telemetry.ToolCallOutcomeUnsupported
		return toolFailure(input, toolName, fmt.Errorf("%w: %s", ErrToolUnsupported, toolName), optional), nil
	}

	invokeCtx := ctx
	if manifest.Timeout > 0 {
		var cancel context.CancelFunc
		invokeCtx, cancel = context.WithTimeout(ctx, manifest.Timeout)
		defer cancel()
	}

	result, err := n.tools.Invoke(invokeCtx, toolName, rawParams)
	if err != nil {
		outcome = classifyToolOutcome(err)
		return toolFailure(input, toolName, err, optional), nil
	}

	zero := intPtr(0)
	outcome = telemetry.ToolCallOutcomeSuccess
	return workflow.NodeOutput{
		Status: workflow.NodeStatusSuccess,
		Artifacts: map[string]string{
			"tool_result":    string(result),
			"last_tool_name": toolName,
		},
		EmittedEvents:   []domain.Event{newToolResultEvent(input, toolName, result, "")},
		DeleteArtifacts: []string{"pending_tool", "pending_tool_params", "pending_tool_optional", "tool_error"},
		SetErrorCount:   zero,
	}, nil
}

func classifyToolOutcome(err error) telemetry.ToolCallOutcome {
	switch {
	case err == nil:
		return telemetry.ToolCallOutcomeSuccess
	case errors.Is(err, context.DeadlineExceeded):
		return telemetry.ToolCallOutcomeTimeout
	case errors.Is(err, context.Canceled):
		return telemetry.ToolCallOutcomeCanceled
	case errors.Is(err, ErrToolUnsupported):
		return telemetry.ToolCallOutcomeUnsupported
	case errors.Is(err, ErrToolDisabled):
		return telemetry.ToolCallOutcomeDisabled
	case errors.Is(err, ErrToolInvalidParams):
		return telemetry.ToolCallOutcomeInvalidParams
	case errors.Is(err, ErrToolInvalidResult):
		return telemetry.ToolCallOutcomeInvalidResult
	case errors.Is(err, ErrToolRetryable):
		return telemetry.ToolCallOutcomeRetryable
	default:
		return telemetry.ToolCallOutcomeFailure
	}
}

func toolFailure(input workflow.NodeInput, toolName string, err error, optional bool) workflow.NodeOutput {
	if optional {
		return workflow.NodeOutput{
			Status: workflow.NodeStatusSuccess,
			Artifacts: map[string]string{
				"tool_error":     err.Error(),
				"last_tool_name": toolName,
			},
			EmittedEvents:   []domain.Event{newToolResultEvent(input, toolName, nil, err.Error())},
			DeleteArtifacts: []string{"pending_tool", "pending_tool_params", "pending_tool_optional", "tool_result"},
		}
	}
	return workflow.NodeOutput{
		Status: workflow.NodeStatusFailure,
		Artifacts: map[string]string{
			"resync_reason": fmt.Sprintf("toolcall %s failed: %v", toolName, err),
		},
		DeleteArtifacts: []string{"pending_tool", "pending_tool_params", "pending_tool_optional", "tool_result", "last_tool_name", "tool_error"},
	}
}

func intPtr(n int) *int { v := n; return &v }

func newToolResultEvent(input workflow.NodeInput, toolName string, result json.RawMessage, errString string) domain.Event {
	taskID := domain.TaskID("")
	if input.Task != nil {
		taskID = input.Task.ID
	}
	now := time.Now()
	return domain.Event{
		ID:         fmt.Sprintf("tool-result:%s:%s:%d", input.State.TaskID, toolName, now.UnixNano()),
		Kind:       domain.EventKindToolResult,
		DeviceID:   input.State.DeviceID,
		OccurredAt: now,
		Payload: domain.ToolResultPayload{
			TaskID:    taskID,
			ToolName:  toolName,
			Result:    append([]byte(nil), result...),
			ErrString: errString,
		},
	}
}
