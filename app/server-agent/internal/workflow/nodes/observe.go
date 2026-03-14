package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

const observeTimeout = 15 * time.Second

// ObserveNode issues device.observe to the agent and stores the raw snapshot
// in state artifacts. Routing (→Decide on success, →Resync on failure) is
// determined by the workflow def in the Runner.
type ObserveNode struct {
	disp dispatcher.Dispatcher
}

func NewObserveNode(disp dispatcher.Dispatcher) *ObserveNode {
	return &ObserveNode{disp: disp}
}

func (n *ObserveNode) Run(ctx context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	cmd := domain.Command{
		ID:       fmt.Sprintf("cmd-%s-observe-%d", input.Task.ID, time.Now().UnixNano()),
		Kind:     domain.CommandKindObserve,
		DeviceID: input.State.DeviceID,
		TaskID:   input.Task.ID,
		Params:   json.RawMessage(`{}`),
		IssuedAt: time.Now(),
	}

	tctx, cancel := context.WithTimeout(ctx, observeTimeout)
	defer cancel()

	ch, err := n.disp.Dispatch(tctx, cmd)
	if err != nil {
		return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil
	}

	select {
	case result, ok := <-ch:
		if !ok || !result.Success {
			return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil
		}
		return workflow.NodeOutput{
			Status: workflow.NodeStatusSuccess,
			Artifacts: map[string]string{
				"last_observe_raw": string(result.Raw),
			},
		}, nil

	case <-tctx.Done():
		return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil
	}
}

func cloneState(s *domain.WorkflowState) *domain.WorkflowState {
	cp := *s
	cp.Artifacts = make(map[string]string, len(s.Artifacts))
	for k, v := range s.Artifacts {
		cp.Artifacts[k] = v
	}
	return &cp
}
