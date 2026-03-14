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

const resyncTimeout = 15 * time.Second

// ResyncNode issues device.observe to re-align with the current UI state.
// Success: updates last_observe_raw, clears resync_reason → def routes to Decide.
// Failure: → def routes to Decide (errorCount increment is Runner's job on failure).
type ResyncNode struct {
	disp dispatcher.Dispatcher
}

func NewResyncNode(disp dispatcher.Dispatcher) *ResyncNode {
	return &ResyncNode{disp: disp}
}

func (n *ResyncNode) Run(ctx context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	cmd := domain.Command{
		ID:       fmt.Sprintf("cmd-%s-resync-%d", input.Task.ID, time.Now().UnixNano()),
		Kind:     domain.CommandKindObserve,
		DeviceID: input.State.DeviceID,
		TaskID:   input.Task.ID,
		Params:   json.RawMessage(`{}`),
		IssuedAt: time.Now(),
	}

	tctx, cancel := context.WithTimeout(ctx, resyncTimeout)
	defer cancel()

	ch, err := n.disp.Dispatch(tctx, cmd)
	if err != nil {
		return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil
	}

	select {
	case result, ok := <-ch:
		if ok && result.Success {
			return workflow.NodeOutput{
				Status: workflow.NodeStatusSuccess,
				Artifacts: map[string]string{
					"last_observe_raw": string(result.Raw),
				},
				DeleteArtifacts: []string{"resync_reason"},
			}, nil
		}
		return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil

	case <-tctx.Done():
		return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil
	}
}
