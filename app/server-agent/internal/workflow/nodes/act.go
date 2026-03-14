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

const executeTimeout = 30 * time.Second

// ActNode reads the pending action from artifacts and issues device.execute.
// On success: clears pending_action, stores pre_action_snapshot and last_execute_raw,
// resets errorCount, transitions to Verify (via def).
// On missing action or failure: returns NodeStatusFailure so the def routes to Resync.
type ActNode struct {
	disp dispatcher.Dispatcher
}

func NewActNode(disp dispatcher.Dispatcher) *ActNode {
	return &ActNode{disp: disp}
}

func (n *ActNode) Run(ctx context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	pendingAction := input.State.Artifacts["pending_action"]
	if pendingAction == "" {
		return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil
	}

	params := json.RawMessage(pendingAction)
	cmd := domain.Command{
		ID:       fmt.Sprintf("cmd-%s-act-%d", input.Task.ID, time.Now().UnixNano()),
		Kind:     domain.CommandKindExecute,
		DeviceID: input.State.DeviceID,
		TaskID:   input.Task.ID,
		Params:   params,
		IssuedAt: time.Now(),
	}

	tctx, cancel := context.WithTimeout(ctx, executeTimeout)
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

		prevObserve := input.State.Artifacts["last_observe_raw"]
		zero := 0
		return workflow.NodeOutput{
			Status: workflow.NodeStatusSuccess,
			Artifacts: map[string]string{
				"last_execute_raw":    string(result.Raw),
				"pre_action_snapshot": prevObserve,
			},
			DeleteArtifacts: []string{"pending_action"},
			SetErrorCount:   &zero,
		}, nil

	case <-tctx.Done():
		return workflow.NodeOutput{Status: workflow.NodeStatusFailure}, nil
	}
}
