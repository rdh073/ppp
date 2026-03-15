package workflow

import (
	"context"
	"encoding/json"
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

// ProcessEvent evaluates event against the current step of state.
// Returns (newState, terminal, error).
//   - newState is nil when the event is irrelevant to the current step.
//   - terminal is true when newState.CurrentStep == "terminal".
//   - Caller is responsible for checkpointing non-nil newState.
func (e *Engine) ProcessEvent(
	ctx context.Context,
	state *domain.WorkflowState,
	workflowName string,
	task *domain.Task,
	event domain.Event,
) (*domain.WorkflowState, bool, error) {
	if state.IsTerminal() {
		return nil, true, nil
	}

	def, err := e.resolveDef(ctx, workflowName)
	if err != nil {
		return nil, false, fmt.Errorf("resolve def %q: %w", workflowName, err)
	}

	stepID := state.CurrentStep
	if stepID == "" {
		stepID = def.Entry
	}
	step, ok := def.Steps[stepID]
	if !ok {
		return nil, false, fmt.Errorf("workflow %q has no step %q", workflowName, stepID)
	}

	// Waiting for a confirming event after an action was dispatched.
	if state.WaitingExpect != nil {
		return e.processWaiting(ctx, state, step, event, task, def)
	}

	// Tick events drive two recovery paths:
	//   1. Inside processWaiting — handled above (deadline enforcement).
	//   2. Retry-pending steps — action failed, RetryCount > 0, no WaitingExpect.
	//      The watchdog fires a tick so retries happen within one tick interval
	//      instead of waiting for the next device-originated event (up to 30 s).
	if event.Kind == domain.EventKindWorkflowTick {
		if state.RetryCount > 0 && step.Action != nil {
			return e.executeStep(ctx, state, step, task, def)
		}
		return nil, false, nil
	}

	// Retry: trigger was already matched once; re-execute the action on the
	// first incoming event (any kind) without re-matching the trigger.
	if state.RetryCount > 0 && step.Action != nil {
		return e.executeStep(ctx, state, step, task, def)
	}

	// Check whether this event activates the current step.
	if !MatchEvent(step.Trigger, event) {
		return nil, false, nil
	}

	return e.executeStep(ctx, state, step, task, def)
}

// executeStep runs the logic for an activated step and returns the resulting state.
func (e *Engine) executeStep(
	ctx context.Context,
	state *domain.WorkflowState,
	step domain.StepDef,
	task *domain.Task,
	def *domain.WorkflowDef,
) (*domain.WorkflowState, bool, error) {
	if step.ToolCall != nil {
		outputs, err := e.runToolCall(ctx, step.ToolCall, state.Inputs)
		if err != nil && !step.ToolCall.Optional {
			return e.handleFailure(state, step)
		}
		// Merge outputs into state before advance(); advance() will clone once.
		// state is not reused by the caller after ProcessEvent returns newState.
		for k, v := range outputs {
			state.Inputs[k] = v
		}
		return e.advance(ctx, state, step.OnSuccess, def, task)
	}

	if step.Action != nil {
		result, err := e.executeAction(ctx, state, task, step.Action)
		if err != nil {
			return e.handleFailure(state, step)
		}
		if step.Expect != nil {
			// Pre-check: if the action's snapshotAfter already matches the
			// expect condition, advance immediately without arming WaitingExpect.
			if SnapshotMatchesExpect(result.Raw, *step.Expect) {
				return e.advance(ctx, state, step.OnSuccess, def, task)
			}
			timeout := parseDuration(step.Timeout, defaultStepTimeout)
			next := cloneState(state)
			exp := *step.Expect
			next.WaitingExpect = &exp
			next.DeadlineAt = time.Now().Add(timeout)
			return next, false, nil
		}
		return e.advance(ctx, state, step.OnSuccess, def, task)
	}

	// Action-less expect (unusual but valid: wait for an event without dispatching).
	if step.Expect != nil {
		timeout := parseDuration(step.Timeout, defaultStepTimeout)
		next := cloneState(state)
		exp := *step.Expect
		next.WaitingExpect = &exp
		next.DeadlineAt = time.Now().Add(timeout)
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
			return e.handleFailure(state, step)
		}
		return nil, false, nil // deadline not yet reached; ignore
	}

	// Deadline expired: treat as step failure.
	if !state.DeadlineAt.IsZero() && time.Now().After(state.DeadlineAt) {
		return e.handleFailure(state, step)
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
			// Needs a device event to activate — stop here.
			break
		}
		// Pure routing steps (no action, no tool call) always wait for a device event.
		// Auto-executing them would cause infinite loops when OnSuccess loops back.
		if step.ToolCall == nil && step.Action == nil {
			break
		}

		if step.ToolCall != nil {
			outputs, err := e.runToolCall(ctx, step.ToolCall, next.Inputs)
			if err != nil && !step.ToolCall.Optional {
				// Tool failed; follow failure routing.
				next.CurrentStep = step.OnFailure
				terminalViaSuccess = false
			} else {
				for k, v := range outputs {
					next.Inputs[k] = v
				}
				next.CurrentStep = step.OnSuccess
			}
			next.RetryCount = 0
			continue
		}

		if step.Action != nil {
			result, err := e.executeAction(ctx, next, task, step.Action)
			if err != nil {
				if next.RetryCount < step.MaxRetry {
					// Keep current step; retry will fire on the next incoming event.
					next.RetryCount++
					break
				}
				next.RetryCount = 0
				next.CurrentStep = step.OnFailure
				terminalViaSuccess = false
				continue
			}
			// Action succeeded.
			next.RetryCount = 0
			if step.Expect != nil {
				// Pre-check: if snapshotAfter already satisfies the expect, advance immediately.
				if SnapshotMatchesExpect(result.Raw, *step.Expect) {
					next.CurrentStep = step.OnSuccess
					continue
				}
				timeout := parseDuration(step.Timeout, defaultStepTimeout)
				exp := *step.Expect
				next.WaitingExpect = &exp
				next.DeadlineAt = time.Now().Add(timeout)
				break // suspended: waiting for the confirming event
			}
			next.CurrentStep = step.OnSuccess
			continue
		}

		// Pure routing step (no action, no tool call): advance immediately.
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

// runToolCall invokes a registered tool and extracts outputs into a string map.
// Returns nil outputs (not an error) when the tool call is optional and succeeds
// with no output mapping.
func (e *Engine) runToolCall(
	ctx context.Context,
	def *domain.ToolCallDef,
	inputs map[string]string,
) (map[string]string, error) {
	if e.tools == nil {
		if def.Optional {
			return nil, nil
		}
		return nil, fmt.Errorf("tool invoker not configured")
	}

	// Build JSON params: interpolate {{input.key}} placeholders.
	paramMap := make(map[string]string, len(def.Params))
	for k, v := range def.Params {
		paramMap[k] = Interpolate(v, inputs)
	}
	raw, err := json.Marshal(paramMap)
	if err != nil {
		return nil, fmt.Errorf("marshal tool params: %w", err)
	}

	result, err := e.tools.Invoke(ctx, def.ToolName, raw)
	if err != nil {
		return nil, err
	}

	if len(def.Outputs) == 0 {
		return nil, nil
	}

	// Extract top-level string values from the result JSON object.
	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(result, &resultMap); err != nil {
		return nil, fmt.Errorf("unmarshal tool result: %w", err)
	}
	outputs := make(map[string]string, len(def.Outputs))
	for resultKey, inputKey := range def.Outputs {
		raw, ok := resultMap[resultKey]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			// Not a JSON string: use raw representation.
			s = string(raw)
		}
		outputs[inputKey] = s
	}
	return outputs, nil
}

// executeAction builds a device command from action, dispatches it, and waits
// for the device response. Returns the CommandResult (including Raw payload) on
// success, or an empty result with a non-nil error on failure.
func (e *Engine) executeAction(
	ctx context.Context,
	state *domain.WorkflowState,
	task *domain.Task,
	action *domain.ActionDef,
) (domain.CommandResult, error) {
	cmd, err := buildCommand(action, state.DeviceID, task.ID, state.Inputs)
	if err != nil {
		return domain.CommandResult{}, fmt.Errorf("build command: %w", err)
	}

	ch, err := e.disp.Dispatch(ctx, cmd)
	if err != nil {
		return domain.CommandResult{}, fmt.Errorf("dispatch: %w", err)
	}

	select {
	case result := <-ch:
		if !result.Success {
			if result.Err != nil {
				return domain.CommandResult{}, fmt.Errorf("device error %d: %s", result.Err.Code, result.Err.Message)
			}
			return domain.CommandResult{}, fmt.Errorf("action returned failure")
		}
		return result, nil
	case <-ctx.Done():
		return domain.CommandResult{}, ctx.Err()
	}
}

func (e *Engine) resolveDef(ctx context.Context, name string) (*domain.WorkflowDef, error) {
	if name != "" {
		if d, err := e.defs.Get(ctx, name); err == nil {
			return d, nil
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
