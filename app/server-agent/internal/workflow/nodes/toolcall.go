package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"text/template"
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
	Name             string
	Provider         string
	ProviderToolName string
	Description      string
	Deterministic    bool
	Timeout          time.Duration
	RetryBudget      int
	InputSchema      json.RawMessage
	OutputSchema     json.RawMessage
	Tags             []string
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

// ToolArtifactBinding declares how a tool binding writes workflow artifacts.
type ToolArtifactBinding struct {
	Artifact        string
	FromJSONPointer string
	Template        string
	Value           *string
}

// ToolBinding declares a workflow-facing tool queue item that can render params
// and map a tool result back into workflow artifacts declaratively.
type ToolBinding struct {
	ID               string
	ToolName         string
	Optional         bool
	Constants        map[string]any
	ParamsTemplate   string
	SuccessArtifacts []ToolArtifactBinding
	FailureArtifacts []ToolArtifactBinding
}

// ToolBindingResolver resolves a queued binding id to its runtime contract.
type ToolBindingResolver interface {
	Binding(bindingID string) (ToolBinding, bool)
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

// StaticToolBindingResolver is an in-memory binding catalog for tests and
// startup-loaded configuration.
type StaticToolBindingResolver struct {
	bindings map[string]ToolBinding
}

type retryableToolError struct {
	err error
}

type queuedToolInvocation struct {
	toolName         string
	bindingID        string
	optional         bool
	rawParams        json.RawMessage
	successArtifacts []ToolArtifactBinding
	failureArtifacts []ToolArtifactBinding
	constants        map[string]any
	deleteArtifacts  []string
}

type bindingTemplateContext struct {
	Artifacts map[string]string
	Constants map[string]any
	Result    any
	ToolName  string
	BindingID string
	ToolError string
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

func NewStaticToolBindingResolver(bindings ...ToolBinding) StaticToolBindingResolver {
	byID := make(map[string]ToolBinding, len(bindings))
	for _, binding := range bindings {
		if binding.ID == "" {
			panic("nodes.NewStaticToolBindingResolver: binding id required")
		}
		if binding.ToolName == "" {
			panic("nodes.NewStaticToolBindingResolver: binding tool name required")
		}
		if _, exists := byID[binding.ID]; exists {
			panic("nodes.NewStaticToolBindingResolver: duplicate binding id: " + binding.ID)
		}
		byID[binding.ID] = cloneBinding(binding)
	}
	return StaticToolBindingResolver{bindings: byID}
}

func (r StaticToolRegistry) Manifest(toolName string) (ToolManifest, bool) {
	def, ok := r.defs[toolName]
	if !ok {
		return ToolManifest{}, false
	}
	return cloneManifest(def.Manifest), true
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

func (r StaticToolBindingResolver) Binding(bindingID string) (ToolBinding, bool) {
	binding, ok := r.bindings[bindingID]
	if !ok {
		return ToolBinding{}, false
	}
	return cloneBinding(binding), true
}

// ToolCallNode reads the preferred pending_tool_binding artifact or the legacy
// pending_tool/pending_tool_params artifacts, invokes the ToolRegistry, stores
// the raw result as tool_result, and optionally maps that result back into
// workflow artifacts.
// Routing (->Decide on success, ->Resync on failure) is handled by the Runner via the def.
type ToolCallNode struct {
	tools    ToolRegistry
	bindings ToolBindingResolver
	metrics  *telemetry.Registry
}

func NewToolCallNode(tools ToolRegistry, metrics ...*telemetry.Registry) *ToolCallNode {
	return NewToolCallNodeWithBindings(tools, nil, metrics...)
}

func NewToolCallNodeWithBindings(tools ToolRegistry, bindings ToolBindingResolver, metrics ...*telemetry.Registry) *ToolCallNode {
	var registry *telemetry.Registry
	if len(metrics) > 0 {
		registry = metrics[0]
	}
	return &ToolCallNode{tools: tools, bindings: bindings, metrics: registry}
}

func (n *ToolCallNode) Run(ctx context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	invocation, ok, err := n.resolveInvocation(input.State.Artifacts)
	if err != nil {
		return workflow.NodeOutput{
			Status: workflow.NodeStatusFailure,
			Artifacts: map[string]string{
				"resync_reason": fmt.Sprintf("toolcall binding resolve failed: %v", err),
			},
			DeleteArtifacts: []string{"pending_tool_binding", "pending_tool", "pending_tool_params", "pending_tool_optional", "tool_result", "tool_error", "last_tool_name", "last_tool_binding"},
		}, nil
	}
	if !ok {
		// Nothing queued - report success; def will route to Decide.
		return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
	}

	toolName := invocation.toolName
	startedAt := time.Now()
	outcome := telemetry.ToolCallOutcomeFailure
	defer func() {
		if n.metrics == nil {
			return
		}
		n.metrics.ObserveToolCall(toolName, outcome, time.Since(startedAt))
	}()

	manifest, ok := n.tools.Manifest(toolName)
	if !ok {
		outcome = telemetry.ToolCallOutcomeUnsupported
		return toolFailure(input, invocation, fmt.Errorf("%w: %s", ErrToolUnsupported, toolName)), nil
	}

	invokeCtx := ctx
	if manifest.Timeout > 0 {
		var cancel context.CancelFunc
		invokeCtx, cancel = context.WithTimeout(ctx, manifest.Timeout)
		defer cancel()
	}

	result, err := n.tools.Invoke(invokeCtx, toolName, invocation.rawParams)
	if err != nil {
		outcome = classifyToolOutcome(err)
		return toolFailure(input, invocation, err), nil
	}

	artifacts := map[string]string{
		"tool_result":    string(result),
		"last_tool_name": toolName,
	}
	if invocation.bindingID != "" {
		artifacts["last_tool_binding"] = invocation.bindingID
		updates, err := applyArtifactMappings(invocation.successArtifacts, result, input.State.Artifacts, invocation.constants, toolName, invocation.bindingID, "")
		if err != nil {
			outcome = telemetry.ToolCallOutcomeInvalidResult
			return toolFailure(input, invocation, fmt.Errorf("%w: %v", ErrToolInvalidResult, err)), nil
		}
		for k, v := range updates {
			artifacts[k] = v
		}
	}

	zero := intPtr(0)
	outcome = telemetry.ToolCallOutcomeSuccess
	return workflow.NodeOutput{
		Status:          workflow.NodeStatusSuccess,
		Artifacts:       artifacts,
		EmittedEvents:   []domain.Event{newToolResultEvent(input, toolName, invocation.bindingID, result, "")},
		DeleteArtifacts: append([]string(nil), invocation.deleteArtifacts...),
		SetErrorCount:   zero,
	}, nil
}

func (n *ToolCallNode) resolveInvocation(artifacts map[string]string) (queuedToolInvocation, bool, error) {
	if bindingID := strings.TrimSpace(artifacts["pending_tool_binding"]); bindingID != "" {
		if n.bindings == nil {
			return queuedToolInvocation{}, false, fmt.Errorf("binding resolver unavailable for %q", bindingID)
		}
		binding, ok := n.bindings.Binding(bindingID)
		if !ok {
			return queuedToolInvocation{}, false, fmt.Errorf("binding %q not found", bindingID)
		}
		rawParams, err := renderBindingParams(binding, artifacts)
		if err != nil {
			return queuedToolInvocation{}, false, err
		}
		optional := binding.Optional || artifacts["pending_tool_optional"] == "true"
		return queuedToolInvocation{
			toolName:         binding.ToolName,
			bindingID:        binding.ID,
			optional:         optional,
			rawParams:        rawParams,
			successArtifacts: cloneMappings(binding.SuccessArtifacts),
			failureArtifacts: cloneMappings(binding.FailureArtifacts),
			constants:        cloneConstants(binding.Constants),
			deleteArtifacts:  []string{"pending_tool_binding", "pending_tool", "pending_tool_params", "pending_tool_optional", "tool_error"},
		}, true, nil
	}

	toolName := strings.TrimSpace(artifacts["pending_tool"])
	if toolName == "" {
		return queuedToolInvocation{}, false, nil
	}
	rawParams := json.RawMessage(artifacts["pending_tool_params"])
	if len(rawParams) == 0 {
		rawParams = json.RawMessage(`{}`)
	}
	return queuedToolInvocation{
		toolName:        toolName,
		optional:        artifacts["pending_tool_optional"] == "true",
		rawParams:       append(json.RawMessage(nil), rawParams...),
		deleteArtifacts: []string{"pending_tool", "pending_tool_params", "pending_tool_optional", "tool_error"},
	}, true, nil
}

func renderBindingParams(binding ToolBinding, artifacts map[string]string) (json.RawMessage, error) {
	tpl := strings.TrimSpace(binding.ParamsTemplate)
	if tpl == "" {
		return json.RawMessage(`{}`), nil
	}
	rendered, err := renderTemplate(tpl, bindingTemplateContext{
		Artifacts: cloneStringMap(artifacts),
		Constants: cloneConstants(binding.Constants),
	})
	if err != nil {
		return nil, fmt.Errorf("render params for binding %s: %w", binding.ID, err)
	}
	trimmed := strings.TrimSpace(rendered)
	if !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("binding %s params template rendered invalid JSON", binding.ID)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(trimmed)); err != nil {
		return nil, fmt.Errorf("binding %s params compact failed: %w", binding.ID, err)
	}
	return json.RawMessage(compact.Bytes()), nil
}

func applyArtifactMappings(
	mappings []ToolArtifactBinding,
	rawResult json.RawMessage,
	artifacts map[string]string,
	constants map[string]any,
	toolName string,
	bindingID string,
	toolError string,
) (map[string]string, error) {
	updates := make(map[string]string, len(mappings))
	if len(mappings) == 0 {
		return updates, nil
	}

	var result any
	if len(rawResult) > 0 {
		decoded, err := decodeJSONAny(rawResult)
		if err != nil {
			return nil, fmt.Errorf("decode tool result: %w", err)
		}
		result = decoded
	}

	ctx := bindingTemplateContext{
		Artifacts: cloneStringMap(artifacts),
		Constants: cloneConstants(constants),
		Result:    result,
		ToolName:  toolName,
		BindingID: bindingID,
		ToolError: toolError,
	}

	for _, mapping := range mappings {
		if mapping.Artifact == "" {
			return nil, fmt.Errorf("artifact mapping missing artifact name")
		}
		value, err := resolveArtifactMapping(mapping, ctx)
		if err != nil {
			return nil, fmt.Errorf("artifact %s: %w", mapping.Artifact, err)
		}
		updates[mapping.Artifact] = value
	}
	return updates, nil
}

func resolveArtifactMapping(mapping ToolArtifactBinding, ctx bindingTemplateContext) (string, error) {
	sources := 0
	if mapping.FromJSONPointer != "" {
		sources++
	}
	if mapping.Template != "" {
		sources++
	}
	if mapping.Value != nil {
		sources++
	}
	if sources != 1 {
		return "", fmt.Errorf("expected exactly one source (fromJsonPointer, template, value)")
	}
	if mapping.Value != nil {
		return *mapping.Value, nil
	}
	if mapping.Template != "" {
		rendered, err := renderTemplate(mapping.Template, ctx)
		if err != nil {
			return "", err
		}
		return rendered, nil
	}
	if ctx.Result == nil {
		return "", fmt.Errorf("fromJsonPointer requires tool result")
	}
	value, err := jsonPointerValue(ctx.Result, mapping.FromJSONPointer)
	if err != nil {
		return "", err
	}
	return artifactString(value)
}

func renderTemplate(src string, ctx bindingTemplateContext) (string, error) {
	funcs := template.FuncMap{
		"artifact": func(key string) string {
			return ctx.Artifacts[key]
		},
		"constant": func(key string) any {
			return ctx.Constants[key]
		},
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
		"default": func(def any, value any) any {
			if isTemplateZero(value) {
				return def
			}
			return value
		},
	}
	tpl, err := template.New("tool-binding").Funcs(funcs).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func decodeJSONAny(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func jsonPointerValue(doc any, pointer string) (any, error) {
	if pointer == "" {
		return doc, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("json pointer must start with /")
	}
	current := doc
	for _, rawToken := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(rawToken, "~1", "/"), "~0", "~")
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[token]
			if !ok {
				return nil, fmt.Errorf("pointer %s missing key %q", pointer, token)
			}
			current = value
		case []any:
			idx, err := strconv.Atoi(token)
			if err != nil {
				return nil, fmt.Errorf("pointer %s requires numeric index, got %q", pointer, token)
			}
			if idx < 0 || idx >= len(node) {
				return nil, fmt.Errorf("pointer %s index %d out of range", pointer, idx)
			}
			current = node[idx]
		default:
			return nil, fmt.Errorf("pointer %s reached scalar before %q", pointer, token)
		}
	}
	return current, nil
}

func artifactString(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case json.Number:
		return v.String(), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
}

func isTemplateZero(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	}
	return false
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

func toolFailure(input workflow.NodeInput, invocation queuedToolInvocation, err error) workflow.NodeOutput {
	if invocation.optional {
		artifacts := map[string]string{
			"tool_error":     err.Error(),
			"last_tool_name": invocation.toolName,
		}
		if invocation.bindingID != "" {
			artifacts["last_tool_binding"] = invocation.bindingID
			updates, mapErr := applyArtifactMappings(invocation.failureArtifacts, nil, input.State.Artifacts, invocation.constants, invocation.toolName, invocation.bindingID, err.Error())
			if mapErr != nil {
				return workflow.NodeOutput{
					Status: workflow.NodeStatusFailure,
					Artifacts: map[string]string{
						"resync_reason": fmt.Sprintf("toolcall %s failure mapping failed: %v", invocation.toolName, mapErr),
					},
					DeleteArtifacts: []string{"pending_tool_binding", "pending_tool", "pending_tool_params", "pending_tool_optional", "tool_result", "last_tool_name", "last_tool_binding", "tool_error"},
				}
			}
			for k, v := range updates {
				artifacts[k] = v
			}
		}
		deleteArtifacts := append([]string(nil), invocation.deleteArtifacts...)
		deleteArtifacts = append(deleteArtifacts, "tool_result")
		return workflow.NodeOutput{
			Status:          workflow.NodeStatusSuccess,
			Artifacts:       artifacts,
			EmittedEvents:   []domain.Event{newToolResultEvent(input, invocation.toolName, invocation.bindingID, nil, err.Error())},
			DeleteArtifacts: deleteArtifacts,
		}
	}
	return workflow.NodeOutput{
		Status: workflow.NodeStatusFailure,
		Artifacts: map[string]string{
			"resync_reason": fmt.Sprintf("toolcall %s failed: %v", invocation.toolName, err),
		},
		DeleteArtifacts: []string{"pending_tool_binding", "pending_tool", "pending_tool_params", "pending_tool_optional", "tool_result", "last_tool_name", "last_tool_binding", "tool_error"},
	}
}

func intPtr(n int) *int { v := n; return &v }

func newToolResultEvent(input workflow.NodeInput, toolName string, bindingID string, result json.RawMessage, errString string) domain.Event {
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
			BindingID: bindingID,
			Result:    append([]byte(nil), result...),
			ErrString: errString,
		},
	}
}

func cloneManifest(manifest ToolManifest) ToolManifest {
	manifest.InputSchema = append(json.RawMessage(nil), manifest.InputSchema...)
	manifest.OutputSchema = append(json.RawMessage(nil), manifest.OutputSchema...)
	manifest.Tags = append([]string(nil), manifest.Tags...)
	return manifest
}

func cloneBinding(binding ToolBinding) ToolBinding {
	binding.Constants = cloneConstants(binding.Constants)
	binding.SuccessArtifacts = cloneMappings(binding.SuccessArtifacts)
	binding.FailureArtifacts = cloneMappings(binding.FailureArtifacts)
	return binding
}

func cloneMappings(in []ToolArtifactBinding) []ToolArtifactBinding {
	out := make([]ToolArtifactBinding, len(in))
	for i, mapping := range in {
		out[i] = mapping
		if mapping.Value != nil {
			value := *mapping.Value
			out[i].Value = &value
		}
	}
	return out
}

func cloneConstants(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
