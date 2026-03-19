package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolCallNode handles steps with a non-nil ToolCall field.
// It interpolates params, invokes the registered tool, and maps result
// fields into NodeOutput.StateInputs.
type ToolCallNode struct {
	tools ToolInvoker
}

func (n *ToolCallNode) Execute(ctx context.Context, cmd NodeCommand) (NodeOutput, error) {
	def := cmd.Step.ToolCall
	inputs := cmd.State.Inputs

	if n.tools == nil {
		if def.Optional {
			return NodeOutput{}, nil
		}
		return NodeOutput{Err: fmt.Errorf("tool invoker not configured")}, nil
	}

	// Build JSON params: interpolate {{input.key}} placeholders.
	//
	// For literal (non-template) values we allow JSON scalar/object decoding so
	// YAML params like `minAge: 25` or `includeSymbols: true` are sent as number
	// / boolean instead of strings. For templated values we keep strings.
	paramMap := make(map[string]any, len(def.Params))
	for k, v := range def.Params {
		paramMap[k] = interpolateToolParam(v, inputs)
	}
	raw, err := json.Marshal(paramMap)
	if err != nil {
		return NodeOutput{Err: fmt.Errorf("marshal tool params: %w", err)}, nil
	}

	result, err := n.tools.Invoke(ctx, def.ToolName, raw)
	if err != nil {
		if def.Optional {
			return NodeOutput{}, nil
		}
		return NodeOutput{Err: err}, nil
	}

	if len(def.Outputs) == 0 {
		return NodeOutput{}, nil
	}

	// Extract top-level string values from the result JSON object.
	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(result, &resultMap); err != nil {
		return NodeOutput{Err: fmt.Errorf("unmarshal tool result: %w", err)}, nil
	}
	outputs := make(map[string]string, len(def.Outputs))
	for resultKey, inputKey := range def.Outputs {
		rawVal, ok := resultMap[resultKey]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(rawVal, &s); err != nil {
			// Not a JSON string: use raw representation.
			s = string(rawVal)
		}
		outputs[inputKey] = s
	}
	return NodeOutput{StateInputs: outputs}, nil
}

func interpolateToolParam(template string, inputs map[string]string) any {
	interpolated := Interpolate(template, inputs)
	if strings.Contains(template, "{{") {
		return interpolated
	}
	trimmed := strings.TrimSpace(interpolated)
	if trimmed == "" {
		return interpolated
	}
	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return interpolated
	}
	switch decoded.(type) {
	case bool, float64, nil, map[string]any, []any, string:
		return decoded
	default:
		return interpolated
	}
}

// Ensure ToolCallNode implements Node at compile time.
var _ Node = (*ToolCallNode)(nil)
