package nodes

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// ToolRegistry is the dependency surface for external tool invocations.
// Implementations may call LLMs, REST APIs, or local functions.
// The no-op implementation is used at MVP.
type ToolRegistry interface {
	// Invoke calls the named tool with the given JSON params and returns a JSON result.
	Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error)
}

// NoopToolRegistry satisfies ToolRegistry with stub responses.
type NoopToolRegistry struct{}

func (NoopToolRegistry) Invoke(_ context.Context, toolName string, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(fmt.Sprintf(`{"tool":%q,"status":"noop"}`, toolName)), nil
}

// ToolCallNode reads the pending_tool and pending_tool_params artifacts, invokes
// the ToolRegistry, stores the result as tool_result.
// Routing (→Decide on success, →Resync on failure) is handled by the Runner via the def.
type ToolCallNode struct {
	tools ToolRegistry
}

func NewToolCallNode(tools ToolRegistry) *ToolCallNode {
	return &ToolCallNode{tools: tools}
}

func (n *ToolCallNode) Run(ctx context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	toolName := input.State.Artifacts["pending_tool"]
	if toolName == "" {
		// Nothing queued — report success; def will route to Decide.
		return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
	}

	rawParams := json.RawMessage(input.State.Artifacts["pending_tool_params"])
	if len(rawParams) == 0 {
		rawParams = json.RawMessage(`{}`)
	}

	result, err := n.tools.Invoke(ctx, toolName, rawParams)
	if err != nil {
		return workflow.NodeOutput{
			Status: workflow.NodeStatusFailure,
			Artifacts: map[string]string{
				"resync_reason": fmt.Sprintf("toolcall %s failed: %v", toolName, err),
			},
			DeleteArtifacts: []string{"pending_tool", "pending_tool_params"},
		}, nil
	}

	zero := intPtr(0)
	return workflow.NodeOutput{
		Status: workflow.NodeStatusSuccess,
		Artifacts: map[string]string{
			"tool_result": string(result),
		},
		DeleteArtifacts: []string{"pending_tool", "pending_tool_params"},
		SetErrorCount:   zero,
	}, nil
}

func intPtr(n int) *int { v := n; return &v }
