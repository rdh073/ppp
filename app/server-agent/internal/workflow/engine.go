package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const defaultStepTimeout = 10 * time.Second

// ToolInvoker is the port the engine uses to call registered tools.
// It is satisfied by tools.ToolRegistry (outer layer); the engine never
// imports the tools package directly.
type ToolInvoker interface {
	Invoke(ctx context.Context, toolName string, params json.RawMessage) (json.RawMessage, error)
}

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
	disp  dispatcher.Dispatcher
	tools ToolInvoker // nil when no tool catalog is wired
}

// NewEngine creates an Engine. tools is optional; pass nil or omit to disable
// tool-call steps (they will follow OnFailure or skip if Optional).
func NewEngine(defs DefStore, disp dispatcher.Dispatcher, tools ...ToolInvoker) *Engine {
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
		next := cloneState(state)
		for k, v := range outputs {
			next.Inputs[k] = v
		}
		return e.advance(ctx, next, step.OnSuccess, def, task)
	}

	if step.Action != nil {
		if err := e.executeAction(ctx, state, task, step.Action); err != nil {
			return e.handleFailure(state, step)
		}
	}

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
		if !ok || !isTriggerEmpty(step.Trigger) {
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
			if err := e.executeAction(ctx, next, task, step.Action); err != nil {
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
// for the device response. Returns nil on success, error on failure.
func (e *Engine) executeAction(
	ctx context.Context,
	state *domain.WorkflowState,
	task *domain.Task,
	action *domain.ActionDef,
) error {
	cmd, err := buildCommand(action, state.DeviceID, task.ID, state.Inputs)
	if err != nil {
		return fmt.Errorf("build command: %w", err)
	}

	ch, err := e.disp.Dispatch(ctx, cmd)
	if err != nil {
		return fmt.Errorf("dispatch: %w", err)
	}

	select {
	case result := <-ch:
		if !result.Success {
			if result.Err != nil {
				return fmt.Errorf("device error %d: %s", result.Err.Code, result.Err.Message)
			}
			return fmt.Errorf("action returned failure")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
	return nil, fmt.Errorf("workflow def %q not found", name)
}

// --- helpers ---

// isTriggerEmpty reports whether m has no filtering criteria.
// An empty trigger auto-executes when the step is reached via advance().
func isTriggerEmpty(m domain.EventMatch) bool {
	return m.Kind == "" && m.Package == "" && m.ClassSuffix == "" && m.TextContains == ""
}

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

// --- command builder ---

// executeParams is the JSON body for device.execute.
type executeParams struct {
	Action executeAction `json:"action"`
}

type executeAction struct {
	Kind      string         `json:"kind"`
	Target    *executeTarget `json:"target,omitempty"`
	InputText string         `json:"inputText,omitempty"`
	Package   string         `json:"package,omitempty"`
	Direction string         `json:"direction,omitempty"`
}

type executeTarget struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func buildCommand(
	action *domain.ActionDef,
	deviceID domain.DeviceID,
	taskID domain.TaskID,
	inputs map[string]string,
) (domain.Command, error) {
	cmdKind := domain.CommandKindExecute
	if action.Kind == domain.ActionKindObserve {
		cmdKind = domain.CommandKindObserve
	}

	var params json.RawMessage
	var err error

	if cmdKind == domain.CommandKindObserve {
		params = json.RawMessage(`{}`)
	} else {
		act := executeAction{Kind: string(action.Kind)}
		if action.Target != nil {
			act.Target = &executeTarget{
				Kind:  string(action.Target.Kind),
				Value: Interpolate(action.Target.Value, inputs),
			}
		}
		act.InputText = Interpolate(action.InputText, inputs)
		act.Package = Interpolate(action.Package, inputs)
		act.Direction = action.Direction

		params, err = json.Marshal(executeParams{Action: act})
		if err != nil {
			return domain.Command{}, fmt.Errorf("marshal action params: %w", err)
		}
	}

	return domain.Command{
		ID:       newCommandID(),
		Kind:     cmdKind,
		DeviceID: deviceID,
		TaskID:   taskID,
		Params:   params,
		IssuedAt: time.Now(),
	}, nil
}

// newCommandID generates a random command ID.
func newCommandID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("cmd-%d", time.Now().UnixNano())
	}
	return "cmd-" + hex.EncodeToString(b)
}
