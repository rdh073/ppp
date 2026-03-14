package nodes

import (
	"context"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// DecideNode is a lightweight artifact planner. Most routing still lives in the
// workflow def, but specific built-in workflows may materialize artifacts here
// so the def can route to ToolCall, Act, or Terminal deterministically.
type DecideNode struct{}

func NewDecideNode() *DecideNode { return &DecideNode{} }

func (n *DecideNode) Run(_ context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	return runWorkflowDecision(input)
}
