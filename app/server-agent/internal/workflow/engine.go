package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const defaultStepTimeout = 10 * time.Second

// Engine drives a WorkflowDef step graph against incoming device events.
// ProcessEvent is the single entry point and is safe to call from the
// orchestrator's per-device goroutine.
//
// Step execution model
//
//	Trigger non-empty → wait for a matching device event to activate the step.
//	Trigger empty     → step is auto-executed immediately when reached via advance().
//
// For each activated step:
//
//	ToolCall set → invoke the tool, merge outputs into Inputs, advance to OnSuccess.
//	Action set   → dispatch device command, arm Expect (if defined), suspend until
//	               the confirming event arrives or the deadline passes.
//	Neither      → pure routing step; advance to OnSuccess immediately.
//
// "UI no transition" handling: every Action step arms an Expect with a Timeout.
// If the Expect event never arrives before the deadline, the next incoming event
// triggers a handleFailure which retries the step up to MaxRetry times, then
// follows OnFailure.
type Engine struct {
	defs  DefStore
	disp  ActionDispatcher
	tools ToolInvoker // nil when no tool catalog is wired
}

// NewEngine creates an Engine. tools is optional; pass nil or omit to disable
// tool-call steps (they will follow OnFailure or skip if Optional).
func NewEngine(defs DefStore, disp ActionDispatcher, tools ...ToolInvoker) *Engine {
	var inv ToolInvoker
	if len(tools) > 0 {
		inv = tools[0]
	}
	return &Engine{defs: defs, disp: disp, tools: inv}
}

// EngineCommand is the input to the workflow engine.
// It bundles all context the engine needs so the API is self-documenting
// and callers cannot accidentally omit required fields.
type EngineCommand struct {
	State        *domain.WorkflowState
	WorkflowName string
	Task         *domain.Task
	Event        domain.Event
}

// EngineResult is returned by Handle.
type EngineResult struct {
	State    *domain.WorkflowState // nil when the event is irrelevant to the current step
	Terminal bool
}

// Handle processes one command against the workflow state machine.
// It is the single entry point for the engine.
//   - result.State is nil when the event is irrelevant to the current step.
//   - result.Terminal is true when the new state's CurrentStep == "terminal".
//   - Caller is responsible for checkpointing non-nil result.State.
func (e *Engine) Handle(ctx context.Context, cmd EngineCommand) (EngineResult, error) {
	state, workflowName, task, event := cmd.State, cmd.WorkflowName, cmd.Task, cmd.Event

	if state.IsTerminal() {
		return EngineResult{Terminal: true}, nil
	}

	def, err := e.resolveDef(ctx, workflowName)
	if err != nil {
		return EngineResult{}, fmt.Errorf("resolve def %q: %w", workflowName, err)
	}

	stepID := state.CurrentStep
	if stepID == "" {
		stepID = def.Entry
	}
	step, ok := def.Steps[stepID]
	if !ok {
		return EngineResult{}, fmt.Errorf("workflow %q has no step %q", workflowName, stepID)
	}

	// Waiting for a confirming event after an action was dispatched.
	if state.WaitingExpect != nil {
		newState, terminal, err := e.processWaiting(ctx, state, step, event, task, def)
		return EngineResult{State: newState, Terminal: terminal}, err
	}

	// Tick events drive two recovery paths:
	//   1. Inside processWaiting — handled above (deadline enforcement).
	//   2. Retry-pending steps — action failed, RetryCount > 0, no WaitingExpect.
	//      The watchdog fires a tick so retries happen within one tick interval
	//      instead of waiting for the next device-originated event (up to 30 s).
	if event.Kind == domain.EventKindWorkflowTick {
		if state.RetryCount > 0 && step.Action != nil {
			newState, terminal, err := e.executeStep(ctx, state, step, task, def)
			return EngineResult{State: newState, Terminal: terminal}, err
		}
		return EngineResult{}, nil
	}

	// Retry: trigger was already matched once; re-execute the action on the
	// first incoming event (any kind) without re-matching the trigger.
	if state.RetryCount > 0 && step.Action != nil {
		newState, terminal, err := e.executeStep(ctx, state, step, task, def)
		return EngineResult{State: newState, Terminal: terminal}, err
	}

	// Check whether this event activates the current step.
	if !MatchEvent(step.Trigger, event) {
		return EngineResult{}, nil
	}

	newState, terminal, err := e.executeStep(ctx, state, step, task, def)
	return EngineResult{State: newState, Terminal: terminal}, err
}

// nodeFor returns the Node responsible for executing step.
func (e *Engine) nodeFor(step domain.StepDef) Node {
	if step.Action != nil {
		return &ActionNode{disp: e.disp}
	}
	if step.ToolCall != nil {
		return &ToolCallNode{tools: e.tools}
	}
	return routingNode{}
}

// routingNode is a no-op Node for pure routing steps (no Action, no ToolCall).
// It returns an empty NodeOutput, which the engine interprets as "advance to OnSuccess".
type routingNode struct{}

func (routingNode) Execute(_ context.Context, _ NodeCommand) (NodeOutput, error) {
	return NodeOutput{}, nil
}

// executeStep runs the logic for an activated step and returns the resulting state.
func (e *Engine) executeStep(
	ctx context.Context,
	state *domain.WorkflowState,
	step domain.StepDef,
	task *domain.Task,
	def *domain.WorkflowDef,
) (*domain.WorkflowState, bool, error) {
	// Action-less expect (unusual but valid: wait for an event without dispatching).
	if step.Action == nil && step.ToolCall == nil && step.Expect != nil {
		timeout := parseDuration(step.Timeout, defaultStepTimeout)
		next := cloneState(state)
		exp := *step.Expect
		next.WaitingExpect = &exp
		next.DeadlineAt = time.Now().Add(timeout)
		return next, false, nil
	}

	out, sysErr := e.nodeFor(step).Execute(ctx, NodeCommand{Step: step, State: state, Task: task})
	if sysErr != nil {
		return nil, false, sysErr
	}

	if out.Err != nil {
		return e.routeFailure(ctx, state, step, def, task)
	}

	// Merge node outputs into state before routing; advance() will clone once.
	// state is not reused by the caller after Handle returns newState.
	for k, v := range out.StateInputs {
		state.Inputs[k] = v
	}

	if out.SuspendExpect != nil {
		next := cloneState(state)
		next.WaitingExpect = out.SuspendExpect
		next.DeadlineAt = time.Now().Add(out.Deadline)
		return next, false, nil
	}
	return e.advance(ctx, state, step.OnSuccess, def, task)
}

// processWaiting evaluates an event against the armed WaitingExpect.
func (e *Engine) processWaiting(
	ctx context.Context,
	state *domain.WorkflowState,
	step domain.StepDef,
	event domain.Event,
	task *domain.Task,
	def *domain.WorkflowDef,
) (*domain.WorkflowState, bool, error) {
	// Synthetic tick from the DeadlineWatchdog: check deadline proactively.
	if event.Kind == domain.EventKindWorkflowTick {
		if !state.DeadlineAt.IsZero() && time.Now().After(state.DeadlineAt) {
			// If retry budget remains and there is an action, re-execute immediately
			// rather than deferring to the next device event. This ensures the retry
			// fires within one watchdog interval even when no device event is pending.
			if state.RetryCount < step.MaxRetry && step.Action != nil {
				next := cloneState(state)
				next.WaitingExpect = nil
				next.DeadlineAt = time.Time{}
				next.RetryCount++
				return e.executeStep(ctx, next, step, task, def)
			}
			return e.routeFailure(ctx, state, step, def, task)
		}
		return nil, false, nil // deadline not yet reached; ignore
	}

	// Deadline expired: treat as step failure.
	if !state.DeadlineAt.IsZero() && time.Now().After(state.DeadlineAt) {
		return e.routeFailure(ctx, state, step, def, task)
	}
	// Confirming event arrived.
	if MatchExpect(*state.WaitingExpect, event) {
		next := cloneState(state)
		next.WaitingExpect = nil
		next.DeadlineAt = time.Time{}
		next.RetryCount = 0
		return e.advance(ctx, next, step.OnSuccess, def, task)
	}
	// Not our event.
	return nil, false, nil
}

// advance moves to stepID and auto-executes any empty-trigger steps in sequence.
//
// A step is auto-executed if its Trigger is empty (matches any event). This
// means the step does not wait for a new device event — it runs inline as part
// of the current event's processing cycle. The loop stops when:
//   - the step has a non-empty Trigger (needs a future device event),
//   - an Action step arms a WaitingExpect (suspended until the confirm event),
//   - the step is "terminal", or
//   - an Action step fails all retries and routes to a non-auto step.
func (e *Engine) advance(
	ctx context.Context,
	state *domain.WorkflowState,
	stepID string,
	def *domain.WorkflowDef,
	task *domain.Task,
) (*domain.WorkflowState, bool, error) {
	next := cloneState(state)
	next.CurrentStep = stepID
	next.RetryCount = 0
	next.WaitingExpect = nil
	next.DeadlineAt = time.Time{}

	// terminalViaSuccess tracks whether every routing decision in this loop
	// followed an OnSuccess branch. Flipped to false when a failure branch is taken.
	// The initial stepID always comes from a caller's OnSuccess context.
	terminalViaSuccess := true

	for !next.IsTerminal() {
		step, ok := def.Steps[next.CurrentStep]
		if !ok || !step.Trigger.IsEmpty() {
			break // stop: step waits for a device event to activate
		}
		if step.ToolCall == nil && step.Action == nil {
			break // stop: pure routing step — auto-executing would loop if OnSuccess points back here
		}

		out, sysErr := e.nodeFor(step).Execute(ctx, NodeCommand{Step: step, State: next, Task: task})
		if sysErr != nil {
			return next, next.IsTerminal(), sysErr
		}

		if out.Err != nil {
			if next.RetryCount < step.MaxRetry {
				next.RetryCount++
				break // stop: retry pending, will fire on next tick
			}
			next.RetryCount = 0
			next.CurrentStep = step.OnFailure
			terminalViaSuccess = false
			continue
		}
		for k, v := range out.StateInputs {
			next.Inputs[k] = v
		}
		next.RetryCount = 0
		if out.SuspendExpect != nil {
			next.WaitingExpect = out.SuspendExpect
			next.DeadlineAt = time.Now().Add(out.Deadline)
			break // stop: action dispatched, awaiting confirm event
		}
		next.CurrentStep = step.OnSuccess
	}

	if next.IsTerminal() && terminalViaSuccess {
		next.TerminalSuccess = true
	}
	return next, next.IsTerminal(), nil
}

// handleFailure increments the retry counter or routes to OnFailure.
func (e *Engine) handleFailure(state *domain.WorkflowState, step domain.StepDef) (*domain.WorkflowState, bool, error) {
	next := cloneState(state)
	next.WaitingExpect = nil
	next.DeadlineAt = time.Time{}
	if next.RetryCount < step.MaxRetry {
		next.RetryCount++
		return next, false, nil // caller will checkpoint; retry fires on next device event
	}
	next.RetryCount = 0
	next.CurrentStep = step.OnFailure
	return next, next.IsTerminal(), nil
}

// routeFailure applies retry/on_failure routing and immediately executes
// empty-trigger failure branches in the same cycle.
func (e *Engine) routeFailure(
	ctx context.Context,
	state *domain.WorkflowState,
	step domain.StepDef,
	def *domain.WorkflowDef,
	task *domain.Task,
) (*domain.WorkflowState, bool, error) {
	next, terminal, err := e.handleFailure(state, step)
	if err != nil || terminal || next == nil {
		return next, terminal, err
	}
	if next.CurrentStep == "" {
		return next, false, nil
	}
	target, ok := def.Steps[next.CurrentStep]
	if !ok || !target.Trigger.IsEmpty() {
		return next, false, nil
	}
	return e.advance(ctx, next, next.CurrentStep, def, task)
}

func (e *Engine) resolveDef(ctx context.Context, name string) (*domain.WorkflowDef, error) {
	if name != "" {
		d, err := e.defs.Get(ctx, name)
		if err == nil {
			return d, nil
		}
		if !errors.Is(err, ErrWorkflowDefNotFound) {
			// Propagate genuine store errors; only fall back to "default" on not-found.
			return nil, err
		}
	}
	// Fall back to the "default" workflow def for tasks with no explicit name.
	if d, err := e.defs.Get(ctx, "default"); err == nil {
		return d, nil
	}
	return nil, fmt.Errorf("workflow def %q: %w", name, ErrWorkflowDefNotFound)
}

// --- helpers ---

func cloneState(s *domain.WorkflowState) *domain.WorkflowState {
	next := *s
	next.Inputs = make(map[string]string, len(s.Inputs))
	for k, v := range s.Inputs {
		next.Inputs[k] = v
	}
	if s.WaitingExpect != nil {
		exp := *s.WaitingExpect
		next.WaitingExpect = &exp
	}
	return &next
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
