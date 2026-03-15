package workflow_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/dispatcher"
	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// --- fakes ---

type successDispatcher struct{}

func (successDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	ch := make(chan domain.CommandResult, 1)
	ch <- domain.CommandResult{CommandID: cmd.ID, Success: true}
	close(ch)
	return ch, nil
}
func (successDispatcher) DeliverResponse(domain.CommandResult) {}

var _ dispatcher.Dispatcher = successDispatcher{}

type failDispatcher struct{}

func (failDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	ch := make(chan domain.CommandResult, 1)
	ch <- domain.CommandResult{CommandID: cmd.ID, Success: false}
	close(ch)
	return ch, nil
}
func (failDispatcher) DeliverResponse(domain.CommandResult) {}

type staticToolInvoker struct {
	calls  int
	result json.RawMessage
	err    error
}

func (t *staticToolInvoker) Invoke(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	t.calls++
	return t.result, t.err
}

// --- builder helpers ---

func buildEngine(def *domain.WorkflowDef, disp dispatcher.Dispatcher, tools ...workflow.ToolInvoker) *workflow.Engine {
	mem := workflow.NewMemoryDefStore()
	_ = mem.Put(context.Background(), def.Name, def)
	return workflow.NewEngine(mem, disp, tools...)
}

func freshState(taskID, deviceID string) *domain.WorkflowState {
	return domain.NewWorkflowState(domain.TaskID(taskID), domain.DeviceID(deviceID))
}

func anyEvent() domain.Event {
	return domain.Event{
		ID:      "ev-1",
		Kind:    domain.EventKindScreenChanged,
		SeqNo:   1,
		OccurredAt: time.Now(),
	}
}

func task(id string) *domain.Task {
	return &domain.Task{ID: domain.TaskID(id), Status: domain.TaskStatusRunning}
}

// --- tests ---

// TestEngine_PureRoutingStep verifies that a step with no action and no tool call
// advances to OnSuccess immediately.
func TestEngine_PureRoutingStep(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "pure-route",
		Entry: "start",
		Steps: map[string]domain.StepDef{
			"start": {
				Trigger:   domain.EventMatch{},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "pure-route", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Terminal {
		t.Error("expected terminal=true")
	}
	if !result.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=true on success path")
	}
}

// TestEngine_ActionStepArmsExpect verifies that a step with an action and expect
// suspends (WaitingExpect set) and does not advance until the confirm event.
func TestEngine_ActionStepArmsExpect(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "action-expect",
		Entry: "do_click",
		Steps: map[string]domain.StepDef{
			"do_click": {
				Trigger: domain.EventMatch{},
				Action:  &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
				Expect:  &domain.ExpectDef{Kind: domain.EventKindScreenChanged},
				Timeout: "5s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// First event triggers the step — action dispatched, expect armed.
	r1, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "action-expect", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r1.Terminal {
		t.Error("should not be terminal before expect event")
	}
	if r1.State.WaitingExpect == nil {
		t.Error("WaitingExpect should be set after action dispatch")
	}

	// Confirming event arrives → advance to terminal.
	confirm := domain.Event{ID: "ev-2", Kind: domain.EventKindScreenChanged, SeqNo: 2, OccurredAt: time.Now()}
	r2, err := eng.Handle(context.Background(), workflow.EngineCommand{State: r1.State, WorkflowName: "action-expect", Task: task("t1"), Event: confirm})
	if err != nil {
		t.Fatalf("unexpected error on confirm: %v", err)
	}
	if !r2.Terminal {
		t.Error("expected terminal=true after confirm event")
	}
	if !r2.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=true")
	}
}

// TestEngine_ExpectTimeout retries then follows OnFailure.
func TestEngine_ExpectTimeout(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "timeout-test",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				Trigger:   domain.EventMatch{},
				Action:    &domain.ActionDef{Kind: domain.ActionKindObserve},
				Expect:    &domain.ExpectDef{Kind: "android.ui.snapshot"},
				Timeout:   "1ms", // immediately expired
				MaxRetry:  1,
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// Arm the expect.
	r0, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "timeout-test", Task: task("t1"), Event: anyEvent()})
	mid := r0.State
	if mid == nil || mid.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be armed")
	}

	// Force deadline to be in the past.
	mid.DeadlineAt = time.Now().Add(-1 * time.Second)

	// Next event arrives after deadline → handleFailure → retry (MaxRetry=1, RetryCount was 0).
	r1, err := eng.Handle(context.Background(), workflow.EngineCommand{State: mid, WorkflowName: "timeout-test", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r1.Terminal {
		t.Error("should not be terminal after first timeout (retry budget=1)")
	}
	if r1.State.RetryCount != 1 {
		t.Errorf("expected RetryCount=1, got %d", r1.State.RetryCount)
	}
	if r1.State.WaitingExpect != nil {
		t.Error("WaitingExpect should be cleared after timeout")
	}

	// Arm expect again (retry fires on next event because trigger is empty).
	r2, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: r1.State, WorkflowName: "timeout-test", Task: task("t1"), Event: anyEvent()})
	mid2 := r2.State
	if mid2 == nil || mid2.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be re-armed on retry")
	}
	mid2.DeadlineAt = time.Now().Add(-1 * time.Second)

	// Second timeout — retry budget exhausted → OnFailure = terminal.
	r3, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: mid2, WorkflowName: "timeout-test", Task: task("t1"), Event: anyEvent()})
	if !r3.Terminal {
		t.Error("expected terminal after retry budget exhausted")
	}
	if r3.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=false on failure path")
	}
}

// TestEngine_ToolCallStep_AutoExecutes verifies a tool_call step with empty trigger
// executes immediately when reached via advance() and maps outputs into Inputs.
func TestEngine_ToolCallStep_AutoExecutes(t *testing.T) {
	inv := &staticToolInvoker{
		result: json.RawMessage(`{"fullName":"Budi Santoso","password":"S3cur3!"}`),
	}

	def := &domain.WorkflowDef{
		Name:  "tool-auto",
		Entry: "trigger_step",
		Steps: map[string]domain.StepDef{
			// trigger_step: activated by any event, then advances to gen_identity.
			"trigger_step": {
				Trigger:   domain.EventMatch{},
				OnSuccess: "gen_identity",
				OnFailure: "terminal",
			},
			// gen_identity: empty trigger → auto-executed immediately after trigger_step.
			"gen_identity": {
				Trigger: domain.EventMatch{},
				ToolCall: &domain.ToolCallDef{
					ToolName: "identity.generate",
					Outputs: map[string]string{
						"fullName": "username",
						"password": "password",
					},
				},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}

	eng := buildEngine(def, successDispatcher{}, inv)
	state := freshState("t1", "dev1")

	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "tool-auto", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Terminal {
		t.Error("expected terminal=true after tool call auto-execute")
	}
	if inv.calls != 1 {
		t.Errorf("expected 1 tool call, got %d", inv.calls)
	}
	if result.State.Inputs["username"] != "Budi Santoso" {
		t.Errorf("expected username=Budi Santoso, got %q", result.State.Inputs["username"])
	}
	if result.State.Inputs["password"] != "S3cur3!" {
		t.Errorf("expected password=S3cur3!, got %q", result.State.Inputs["password"])
	}
}

// TestEngine_ToolCallStep_OptionalSkipsOnError verifies that an optional tool call
// that fails is skipped gracefully (OnSuccess path).
func TestEngine_ToolCallStep_OptionalSkipsOnError(t *testing.T) {
	inv := &staticToolInvoker{err: errors.New("tool unavailable")}

	def := &domain.WorkflowDef{
		Name:  "optional-tool",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				Trigger:   domain.EventMatch{},
				OnSuccess: "gen_data",
				OnFailure: "terminal",
			},
			"gen_data": {
				Trigger: domain.EventMatch{},
				ToolCall: &domain.ToolCallDef{
					ToolName: "flaky.tool",
					Optional: true,
				},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}

	eng := buildEngine(def, successDispatcher{}, inv)
	state := freshState("t1", "dev1")

	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "optional-tool", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Terminal {
		t.Error("expected terminal=true: optional tool failure should follow OnSuccess")
	}
	if !result.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=true when optional tool is skipped")
	}
}

// TestEngine_ToolCallStep_FailureFollowsOnFailure verifies that a non-optional tool
// call failure routes to OnFailure.
func TestEngine_ToolCallStep_FailureFollowsOnFailure(t *testing.T) {
	inv := &staticToolInvoker{err: errors.New("tool error")}

	def := &domain.WorkflowDef{
		Name:  "required-tool",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				Trigger:   domain.EventMatch{},
				OnSuccess: "call_tool",
				OnFailure: "terminal",
			},
			"call_tool": {
				Trigger: domain.EventMatch{},
				ToolCall: &domain.ToolCallDef{
					ToolName: "required.tool",
					Optional: false,
				},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}

	eng := buildEngine(def, successDispatcher{}, inv)
	state := freshState("t1", "dev1")

	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "required-tool", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Terminal {
		t.Error("expected terminal=true after tool failure")
	}
	if result.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=false after required tool failure")
	}
}

// TestEngine_ToolCallStep_InterpolatesParams verifies {{input.key}} substitution
// in tool call params.
func TestEngine_ToolCallStep_InterpolatesParams(t *testing.T) {
	var capturedParams json.RawMessage
	inv := &staticToolInvoker{}
	inv.result = json.RawMessage(`{}`)

	captureInvoker := &capturingInvoker{result: json.RawMessage(`{}`), capture: &capturedParams}

	def := &domain.WorkflowDef{
		Name:  "interp-tool",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				Trigger:   domain.EventMatch{},
				OnSuccess: "gen",
				OnFailure: "terminal",
			},
			"gen": {
				Trigger: domain.EventMatch{},
				ToolCall: &domain.ToolCallDef{
					ToolName: "some.tool",
					Params: map[string]string{
						"hostname": "{{input.private_dns_hostname}}",
					},
					Optional: true,
				},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}

	eng := buildEngine(def, successDispatcher{}, captureInvoker)
	state := freshState("t1", "dev1")
	state.Inputs["private_dns_hostname"] = "dns.example.com"

	_, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "interp-tool", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var params map[string]string
	if err := json.Unmarshal(capturedParams, &params); err != nil {
		t.Fatalf("cannot decode captured params: %v", err)
	}
	if params["hostname"] != "dns.example.com" {
		t.Errorf("expected hostname=dns.example.com, got %q", params["hostname"])
	}
}

// TestEngine_NonMatchingTrigger returns nil state when the event doesn't match.
func TestEngine_NonMatchingTrigger(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "specific-trigger",
		Entry: "wait_screen",
		Steps: map[string]domain.StepDef{
			"wait_screen": {
				Trigger:   domain.EventMatch{Kind: "android.activity.created"},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// Send a non-matching event.
	result, err := eng.Handle(context.Background(), workflow.EngineCommand{
		State: state, WorkflowName: "specific-trigger", Task: task("t1"),
		Event: domain.Event{ID: "ev-1", Kind: "android.screen.changed", SeqNo: 1, OccurredAt: time.Now()},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.State != nil {
		t.Error("expected nil state for non-matching event")
	}
}

// TestEngine_ActionAutoExecuteChain verifies multiple empty-trigger action steps
// chain without intermediate events: first activates, second auto-executes.
func TestEngine_ActionAutoExecuteChain(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "action-chain",
		Entry: "start",
		Steps: map[string]domain.StepDef{
			"start": {
				Trigger:   domain.EventMatch{},
				Action:    &domain.ActionDef{Kind: domain.ActionKindObserve},
				Expect:    &domain.ExpectDef{Kind: domain.EventKindScreenChanged},
				Timeout:   "5s",
				OnSuccess: "click_next",
				OnFailure: "terminal",
			},
			// Empty trigger → auto-dispatched when reached via advance().
			"click_next": {
				Trigger: domain.EventMatch{},
				Action:  &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "Next"}},
				Expect:  &domain.ExpectDef{Kind: domain.EventKindScreenChanged},
				Timeout: "5s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// First event: activates start → observe dispatched → WaitingExpect armed for android.screen.changed.
	r1, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "action-chain", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("step 1 error: %v", err)
	}
	if r1.Terminal {
		t.Error("should not be terminal after step 1 action")
	}
	if r1.State.WaitingExpect == nil {
		t.Fatal("WaitingExpect should be armed after step 1 action")
	}
	if r1.State.WaitingExpect.Kind != domain.EventKindScreenChanged {
		t.Errorf("wrong WaitingExpect kind: %q", r1.State.WaitingExpect.Kind)
	}

	// Confirm event for start → advance to click_next → auto-execute → WaitingExpect armed for android.screen.changed.
	confirmObs := domain.Event{ID: "ev-2", Kind: domain.EventKindScreenChanged, SeqNo: 2, OccurredAt: time.Now()}
	r2, err := eng.Handle(context.Background(), workflow.EngineCommand{State: r1.State, WorkflowName: "action-chain", Task: task("t1"), Event: confirmObs})
	if err != nil {
		t.Fatalf("step 2 confirm error: %v", err)
	}
	if r2.Terminal {
		t.Error("should not be terminal after auto-executing click_next")
	}
	if r2.State.WaitingExpect == nil {
		t.Fatal("WaitingExpect should be armed after click_next auto-execute")
	}
	if r2.State.WaitingExpect.Kind != domain.EventKindScreenChanged {
		t.Errorf("wrong WaitingExpect kind: %q", r2.State.WaitingExpect.Kind)
	}

	// Confirm event for click_next → terminal.
	confirmWin := domain.Event{ID: "ev-3", Kind: domain.EventKindScreenChanged, SeqNo: 3, OccurredAt: time.Now()}
	r3, err := eng.Handle(context.Background(), workflow.EngineCommand{State: r2.State, WorkflowName: "action-chain", Task: task("t1"), Event: confirmWin})
	if err != nil {
		t.Fatalf("step 3 confirm error: %v", err)
	}
	if !r3.Terminal {
		t.Error("expected terminal after final confirm")
	}
	if !r3.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=true")
	}
}

// --- extra fakes ---

type capturingInvoker struct {
	result  json.RawMessage
	capture *json.RawMessage
}

func (c *capturingInvoker) Invoke(_ context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
	*c.capture = params
	return c.result, nil
}

// rawDispatcher returns a success result with a preset Raw payload.
type rawDispatcher struct{ raw json.RawMessage }

func (r rawDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	ch := make(chan domain.CommandResult, 1)
	ch <- domain.CommandResult{CommandID: cmd.ID, Success: true, Raw: r.raw}
	close(ch)
	return ch, nil
}
func (r rawDispatcher) DeliverResponse(domain.CommandResult) {}

// --- new tests for Fix 1: snapshot pre-check ---

// TestEngine_SnapshotPreCheck_Match verifies that when the device.execute
// response already satisfies the Expect condition, the engine advances
// immediately without arming WaitingExpect.
func TestEngine_SnapshotPreCheck_Match(t *testing.T) {
	raw := json.RawMessage(`{"snapshotAfter":{"packageName":"com.example.app","activityName":"com.example.app.MainActivity","targets":[]}}`)
	def := &domain.WorkflowDef{
		Name:  "precheck-match",
		Entry: "click",
		Steps: map[string]domain.StepDef{
			"click": {
				Trigger: domain.EventMatch{},
				Action:  &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
				Expect: &domain.ExpectDef{
					Package:     "com.example.app",
					ClassSuffix: "MainActivity",
				},
				Timeout:   "5s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, rawDispatcher{raw: raw})
	state := freshState("t1", "dev1")

	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "precheck-match", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Terminal {
		t.Error("expected terminal=true: snapshot already matched, should advance immediately")
	}
	if result.State.WaitingExpect != nil {
		t.Error("WaitingExpect should NOT be set when snapshot pre-check matches")
	}
	if !result.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=true")
	}
}

// TestEngine_SnapshotPreCheck_NoMatch verifies that when the snapshot does NOT
// satisfy the Expect condition, WaitingExpect is armed as usual.
func TestEngine_SnapshotPreCheck_NoMatch(t *testing.T) {
	raw := json.RawMessage(`{"snapshotAfter":{"packageName":"com.other.app","activityName":"com.other.app.OtherActivity","targets":[]}}`)
	def := &domain.WorkflowDef{
		Name:  "precheck-nomatch",
		Entry: "click",
		Steps: map[string]domain.StepDef{
			"click": {
				Trigger: domain.EventMatch{},
				Action:  &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
				Expect: &domain.ExpectDef{
					Package:     "com.example.app",
					ClassSuffix: "MainActivity",
				},
				Timeout:   "5s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, rawDispatcher{raw: raw})
	state := freshState("t1", "dev1")

	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "precheck-nomatch", Task: task("t1"), Event: anyEvent()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Terminal {
		t.Error("should not be terminal: snapshot did not match, WaitingExpect should be armed")
	}
	if result.State.WaitingExpect == nil {
		t.Error("WaitingExpect should be set when snapshot pre-check does not match")
	}
}

// TestEngine_TickEvent_ClearsExpiredWaiting verifies that a workflow.tick event
// with an expired deadline triggers handleFailure inside processWaiting.
func TestEngine_TickEvent_ClearsExpiredWaiting(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "tick-test",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				Trigger:   domain.EventMatch{},
				Action:    &domain.ActionDef{Kind: domain.ActionKindObserve},
				Expect:    &domain.ExpectDef{Kind: domain.EventKindScreenChanged},
				Timeout:   "1ms",
				MaxRetry:  0,
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// Arm WaitingExpect.
	r0, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "tick-test", Task: task("t1"), Event: anyEvent()})
	mid := r0.State
	if mid == nil || mid.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be armed")
	}
	mid.DeadlineAt = time.Now().Add(-1 * time.Second) // force expired

	// Inject a tick event.
	tick := domain.Event{
		ID:         "tick-1",
		Kind:       domain.EventKindWorkflowTick,
		OccurredAt: time.Now(),
	}
	r1, err := eng.Handle(context.Background(), workflow.EngineCommand{State: mid, WorkflowName: "tick-test", Task: task("t1"), Event: tick})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r1.Terminal {
		t.Error("expected terminal=true after tick fires expired deadline (MaxRetry=0)")
	}
	if r1.State.TerminalSuccess {
		t.Error("expected TerminalSuccess=false on failure path")
	}
}

// TestEngine_TickEvent_NotExpired_Ignored verifies that a tick event with a
// non-expired deadline is a no-op (returns nil state).
func TestEngine_TickEvent_NotExpired_Ignored(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "tick-noop",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				Trigger:   domain.EventMatch{},
				Action:    &domain.ActionDef{Kind: domain.ActionKindObserve},
				Expect:    &domain.ExpectDef{Kind: domain.EventKindScreenChanged},
				Timeout:   "60s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	r0, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "tick-noop", Task: task("t1"), Event: anyEvent()})
	mid := r0.State
	if mid == nil || mid.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be armed")
	}
	// deadline is 60s from now — not expired

	tick := domain.Event{Kind: domain.EventKindWorkflowTick, OccurredAt: time.Now()}
	r1, err := eng.Handle(context.Background(), workflow.EngineCommand{State: mid, WorkflowName: "tick-noop", Task: task("t1"), Event: tick})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r1.Terminal {
		t.Error("should not be terminal: deadline not expired")
	}
	if r1.State != nil {
		t.Error("expected nil state: non-expired tick is a no-op")
	}
}

// TestEngine_TickEvent_WhenNotWaiting_Ignored verifies that tick events are
// ignored when the engine is not in WaitingExpect state.
func TestEngine_TickEvent_WhenNotWaiting_Ignored(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "tick-nowaiting",
		Entry: "wait_activity",
		Steps: map[string]domain.StepDef{
			"wait_activity": {
				Trigger:   domain.EventMatch{Kind: "android.activity.created"},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	tick := domain.Event{Kind: domain.EventKindWorkflowTick, OccurredAt: time.Now()}
	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "tick-nowaiting", Task: task("t1"), Event: tick})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Terminal || result.State != nil {
		t.Error("tick event should be ignored when not in WaitingExpect state")
	}
}

// TestEngine_RetryReExecute_SkipsTrigger verifies that when RetryCount > 0 and
// the step has an action, a non-matching event still re-executes the action
// (trigger is not re-checked during an active retry cycle).
func TestEngine_RetryReExecute_SkipsTrigger(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "retry-reexecute",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				// Specific trigger: only matches "android.activity.created".
				Trigger:   domain.EventMatch{Kind: domain.EventKindActivityCreated},
				Action:    &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
				Expect:    &domain.ExpectDef{Kind: "android.activity.created"},
				Timeout:   "1ms",
				MaxRetry:  2,
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// Activate step with matching trigger.
	activateEv := domain.Event{ID: "ev-1", Kind: domain.EventKindActivityCreated, SeqNo: 1, OccurredAt: time.Now()}
	r0, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "retry-reexecute", Task: task("t1"), Event: activateEv})
	mid := r0.State
	if mid == nil || mid.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be armed after initial activation")
	}
	mid.DeadlineAt = time.Now().Add(-1 * time.Second) // expire deadline

	// Timeout: triggers handleFailure, RetryCount becomes 1.
	r1, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: mid, WorkflowName: "retry-reexecute", Task: task("t1"), Event: anyEvent()})
	retry1 := r1.State
	if retry1 == nil || retry1.RetryCount != 1 {
		t.Fatalf("expected RetryCount=1 after first timeout, got state=%v", retry1)
	}
	if retry1.WaitingExpect != nil {
		t.Error("WaitingExpect should be cleared after timeout")
	}

	// Now send a NON-matching event (kind does not match "android.activity.created").
	// With retry re-execute, the action should fire despite the trigger mismatch.
	nonMatch := domain.Event{ID: "ev-2", Kind: "android.screen.changed", SeqNo: 2, OccurredAt: time.Now()}
	r2, err := eng.Handle(context.Background(), workflow.EngineCommand{State: retry1, WorkflowName: "retry-reexecute", Task: task("t1"), Event: nonMatch})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r2.State == nil {
		t.Fatal("expected non-nil state: retry should re-execute action on any event")
	}
	// Action dispatched (successDispatcher), expect kind-only → WaitingExpect re-armed.
	if r2.State.WaitingExpect == nil {
		t.Error("expected WaitingExpect re-armed after retry re-execute")
	}
}

// TestEngine_TickEvent_TriggersRetry_ForRetryPending verifies that a workflow.tick
// event re-executes the action when the step is in retry state (RetryCount > 0,
// WaitingExpect nil). This ensures retries happen within one watchdog interval
// instead of waiting for the next device-originated event (e.g. heartbeat, 30 s).
func TestEngine_TickEvent_TriggersRetry_ForRetryPending(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "tick-retry",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {
				Trigger:   domain.EventMatch{}, // empty: auto-execute
				Action:    &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
				MaxRetry:  2,
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, failDispatcher{}) // action always fails
	state := freshState("t1", "dev1")

	// Activate: first attempt fails in advance loop → RetryCount=1.
	r0, _ := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "tick-retry", Task: task("t1"), Event: anyEvent()})
	mid := r0.State
	if mid == nil {
		t.Fatal("expected non-nil state after initial activation")
	}
	if mid.RetryCount != 1 {
		t.Fatalf("expected RetryCount=1 after first failure, got %d", mid.RetryCount)
	}
	if mid.WaitingExpect != nil {
		t.Error("WaitingExpect should be nil for failed action (no expect)")
	}

	// Inject a tick: should trigger the retry immediately.
	tick := domain.Event{ID: "tick-1", Kind: domain.EventKindWorkflowTick, OccurredAt: time.Now()}
	r1, err := eng.Handle(context.Background(), workflow.EngineCommand{State: mid, WorkflowName: "tick-retry", Task: task("t1"), Event: tick})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r1.State == nil {
		t.Fatal("expected non-nil state: tick should trigger retry for retry-pending step")
	}
	if r1.State.RetryCount != 2 {
		t.Errorf("expected RetryCount=2 after second failure via tick, got %d", r1.State.RetryCount)
	}

	// Third tick: retry budget exhausted (MaxRetry=2) → OnFailure = terminal.
	tick2 := domain.Event{ID: "tick-2", Kind: domain.EventKindWorkflowTick, OccurredAt: time.Now()}
	r2, err := eng.Handle(context.Background(), workflow.EngineCommand{State: r1.State, WorkflowName: "tick-retry", Task: task("t1"), Event: tick2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r2.Terminal || r2.State == nil {
		t.Error("expected terminal after retry budget exhausted via ticks")
	}
}

// TestEngine_TickEvent_WhenNotWaiting_NoAction_Ignored verifies that tick events
// are still ignored for steps with no Action (pure routing or trigger-only steps).
func TestEngine_TickEvent_WhenNotWaiting_NoAction_Ignored(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "tick-noaction",
		Entry: "wait_activity",
		Steps: map[string]domain.StepDef{
			"wait_activity": {
				Trigger:   domain.EventMatch{Kind: "android.activity.created"},
				OnSuccess: "terminal",
				OnFailure: "terminal",
				// No Action: tick must not fire this step.
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	tick := domain.Event{Kind: domain.EventKindWorkflowTick, OccurredAt: time.Now()}
	result, err := eng.Handle(context.Background(), workflow.EngineCommand{State: state, WorkflowName: "tick-noaction", Task: task("t1"), Event: tick})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Terminal || result.State != nil {
		t.Error("tick must be ignored for steps with no Action")
	}
}

// errorDefStore is a fake DefStore that always returns a non-not-found error.
type errorDefStore struct{ err error }

func (s errorDefStore) Get(_ context.Context, _ string) (*domain.WorkflowDef, error) {
	return nil, s.err
}
func (s errorDefStore) Put(_ context.Context, _ string, _ *domain.WorkflowDef) error { return nil }
func (s errorDefStore) Delete(_ context.Context, _ string) error                     { return nil }
func (s errorDefStore) List(_ context.Context) ([]*domain.WorkflowDef, error)        { return nil, nil }

// TestEngine_ResolveDef_StoreError verifies that a non-ErrWorkflowDefNotFound
// error from the DefStore is propagated instead of silently falling back to "default".
func TestEngine_ResolveDef_StoreError(t *testing.T) {
	storeErr := errors.New("connection refused")
	eng := workflow.NewEngine(errorDefStore{err: storeErr}, successDispatcher{})
	state := freshState("t1", "dev1")

	_, err := eng.Handle(context.Background(), workflow.EngineCommand{
		State:        state,
		WorkflowName: "some-workflow",
		Task:         task("t1"),
		Event:        anyEvent(),
	})
	if err == nil {
		t.Fatal("expected error from DefStore to propagate, got nil")
	}
	if !errors.Is(err, storeErr) {
		t.Fatalf("expected wrapped storeErr, got: %v", err)
	}
}
