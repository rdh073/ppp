package workflow

import (
	"context"
	"fmt"
)

// ActionNode handles steps with a non-nil Action field.
// It dispatches a device command, optionally pre-checks the response
// against an Expect condition, and returns a routing decision.
type ActionNode struct {
	disp ActionDispatcher
}

func (n *ActionNode) Execute(ctx context.Context, cmd NodeCommand) (NodeOutput, error) {
	step := cmd.Step
	state := cmd.State
	task := cmd.Task

	devCmd, err := buildCommand(step.Action, state.DeviceID, task.ID, state.Inputs)
	if err != nil {
		return NodeOutput{Err: fmt.Errorf("build command: %w", err)}, nil
	}

	ch, err := n.disp.Dispatch(ctx, devCmd)
	if err != nil {
		return NodeOutput{Err: fmt.Errorf("dispatch: %w", err)}, nil
	}

	select {
	case result := <-ch:
		if !result.Success {
			if result.Err != nil {
				return NodeOutput{Err: fmt.Errorf("device error %d: %s", result.Err.Code, result.Err.Message)}, nil
			}
			return NodeOutput{Err: fmt.Errorf("action returned failure")}, nil
		}
		if step.Expect != nil {
			if SnapshotMatchesExpect(result.Raw, *step.Expect) {
				// Pre-match: snapshot already satisfies expect; advance immediately.
				return NodeOutput{}, nil
			}
			deadline := parseDuration(step.Timeout, defaultStepTimeout)
			exp := *step.Expect
			return NodeOutput{SuspendExpect: &exp, Deadline: deadline}, nil
		}
		return NodeOutput{}, nil
	case <-ctx.Done():
		return NodeOutput{}, ctx.Err()
	}
}

// Ensure ActionNode implements Node at compile time.
var _ Node = (*ActionNode)(nil)
