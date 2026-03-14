package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// NodeStatus is the outcome of a node execution.
type NodeStatus string

const (
	NodeStatusSuccess NodeStatus = "success"
	NodeStatusFailure NodeStatus = "failure"
	// NodeStatusPending signals that the node is deliberately suspending the
	// workflow until a specific set of device events arrives.
	// The Runner checkpoints the WaitingFor list without evaluating transitions.
	NodeStatusPending NodeStatus = "pending"
)

// NodeInput is everything a node receives to make its decision.
type NodeInput struct {
	Event domain.Event
	State *domain.WorkflowState
	Task  *domain.Task
}

// NodeOutput is the result of executing a node.
// Routing is now the Runner's responsibility, not the node's.
type NodeOutput struct {
	Status          NodeStatus
	Artifacts       map[string]string // keys to merge into state.Artifacts
	DeleteArtifacts []string          // keys to remove from state.Artifacts
	Done            bool              // TerminalNode sets this
	SetErrorCount   *int              // if non-nil, sets state.ErrorCount explicitly
	EmittedEvents   []domain.Event
	// WaitingFor is set when Status == NodeStatusPending.
	// The Runner saves this list to WorkflowState and skips transition evaluation.
	// ProcessEvent will skip this workflow until a matching event kind arrives.
	WaitingFor []domain.EventKind
}

// NodeHandler executes a single workflow node kind.
type NodeHandler interface {
	Run(ctx context.Context, input NodeInput) (NodeOutput, error)
}

// Runner selects and delegates to the correct NodeHandler, then resolves
// the next node via the WorkflowDef obtained from DefStore.
type Runner struct {
	handlers    map[domain.NodeKind]NodeHandler
	defs        DefStore
	defaultName string
}

func NewRunner(handlers map[domain.NodeKind]NodeHandler, defs DefStore, defaultName string) *Runner {
	return &Runner{
		handlers:    handlers,
		defs:        defs,
		defaultName: defaultName,
	}
}

// Run executes the current node, merges artifacts, resolves the next node via
// the workflow def, and returns the updated state.
func (r *Runner) Run(ctx context.Context, input NodeInput) (newState *domain.WorkflowState, done bool, emitted []domain.Event, err error) {
	h, ok := r.handlers[input.State.CurrentNode]
	if !ok {
		return nil, false, nil, fmt.Errorf("no handler for node kind %q", input.State.CurrentNode)
	}

	out, err := h.Run(ctx, input)
	if err != nil {
		return nil, false, nil, fmt.Errorf("node %s: %w", input.State.CurrentNode, err)
	}

	state := cloneState(input.State)
	state.UpdatedAt = time.Now()

	// Merge artifacts.
	for k, v := range out.Artifacts {
		state.Artifacts[k] = v
	}
	for _, k := range out.DeleteArtifacts {
		delete(state.Artifacts, k)
	}

	// Terminal short-circuit.
	if out.Done {
		state.CurrentNode = domain.NodeKindTerminal
		return state, true, out.EmittedEvents, nil
	}

	// Suspend: node is waiting for a specific device event.
	if out.Status == NodeStatusPending {
		state.WaitingFor = out.WaitingFor
		// CurrentNode stays; we'll re-run it when the event arrives.
		return state, false, out.EmittedEvents, nil
	}

	// Clear any prior wait list now that we ran successfully.
	state.WaitingFor = nil

	// Apply error count.
	if out.SetErrorCount != nil {
		state.ErrorCount = *out.SetErrorCount
	} else if out.Status == NodeStatusFailure {
		state.ErrorCount++
	}

	// Resolve the workflow def.
	def := r.resolveDef(ctx, input.Task)
	nodeDef, ok := def.Nodes[input.State.CurrentNode]
	if !ok {
		return nil, false, nil, fmt.Errorf("workflow def %q has no node %q", def.Name, input.State.CurrentNode)
	}

	// Evaluate transitions in order; first match wins.
	// event.kind is available so YAML conditions can branch on the triggering event.
	next, found := r.evalTransitions(nodeDef.Transitions, out.Status, state.Artifacts, state.ErrorCount, input.Event.Kind)
	if !found {
		return nil, false, nil, fmt.Errorf("no matching transition from %q (status=%s)", input.State.CurrentNode, out.Status)
	}

	state.CurrentNode = next
	return state, false, out.EmittedEvents, nil
}

// resolveDef looks up the def for the task's WorkflowName, then the defaultName,
// then falls back to DefaultWorkflowDef.
func (r *Runner) resolveDef(ctx context.Context, task *domain.Task) *domain.WorkflowDef {
	if task != nil && task.WorkflowName != "" {
		if d, err := r.defs.Get(ctx, task.WorkflowName); err == nil {
			return d
		}
	}
	if d, err := r.defs.Get(ctx, r.defaultName); err == nil {
		return d
	}
	return DefaultWorkflowDef
}

func (r *Runner) evalTransitions(
	transitions []domain.Transition,
	status NodeStatus,
	artifacts map[string]string,
	errorCount int,
	eventKind domain.EventKind,
) (domain.NodeKind, bool) {
	for _, t := range transitions {
		if Eval(t.When, status, artifacts, errorCount, eventKind) {
			return t.To, true
		}
	}
	return "", false
}

func cloneState(s *domain.WorkflowState) *domain.WorkflowState {
	cp := *s
	cp.Artifacts = make(map[string]string, len(s.Artifacts))
	for k, v := range s.Artifacts {
		cp.Artifacts[k] = v
	}
	return &cp
}
