package nodes

import (
	"context"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// DecideNode is now a passthrough — all routing logic lives in the workflow def
// evaluated by the Runner. The node simply reports success so the def's
// transition table can route based on artifacts and errorCount.
type DecideNode struct{}

func NewDecideNode() *DecideNode { return &DecideNode{} }

func (n *DecideNode) Run(_ context.Context, _ workflow.NodeInput) (workflow.NodeOutput, error) {
	return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
}
