package workflow

import (
	"context"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// Node handles a single step's execution and returns a routing decision.
// Business failures are returned via NodeOutput.Err.
// System errors (e.g., context cancelled) use the error return value.
type Node interface {
	Execute(ctx context.Context, cmd NodeCommand) (NodeOutput, error)
}

// NodeCommand is the full context a node receives when activated.
type NodeCommand struct {
	Step  domain.StepDef
	State *domain.WorkflowState
	Task  *domain.Task
	// Event is the triggering event; may be zero in auto-advance loops.
	Event domain.Event
}

// NodeOutput is the routing decision returned by a node.
type NodeOutput struct {
	// StateInputs are merged into WorkflowState.Inputs before routing.
	StateInputs map[string]string
	// SuspendExpect, if non-nil, causes the engine to arm WaitingExpect and stop.
	SuspendExpect *domain.ExpectDef
	// Deadline is the timeout for SuspendExpect. Ignored if SuspendExpect is nil.
	Deadline time.Duration
	// Err, if non-nil, causes failure routing (retry or OnFailure).
	// Distinct from Execute's error return, which is reserved for system errors.
	Err error
}
