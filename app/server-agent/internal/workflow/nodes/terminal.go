package nodes

import (
	"context"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// TerminalNode marks the workflow as done. It sets Done=true so the
// Runner and orchestrator stop processing events for this task.
type TerminalNode struct{}

func NewTerminalNode() *TerminalNode { return &TerminalNode{} }

func (n *TerminalNode) Run(_ context.Context, _ workflow.NodeInput) (workflow.NodeOutput, error) {
	return workflow.NodeOutput{Done: true}, nil
}
