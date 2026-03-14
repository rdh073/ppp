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
		Kind:    "android.window.state_changed",
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

	newState, terminal, err := eng.ProcessEvent(context.Background(), state, "pure-route", task("t1"), anyEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !terminal {
		t.Error("expected terminal=true")
	}
	if !newState.TerminalSuccess {
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
				Expect:  &domain.ExpectDef{Kind: "android.window.state_changed"},
				Timeout: "5s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// First event triggers the step — action dispatched, expect armed.
	mid, terminal, err := eng.ProcessEvent(context.Background(), state, "action-expect", task("t1"), anyEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if terminal {
		t.Error("should not be terminal before expect event")
	}
	if mid.WaitingExpect == nil {
		t.Error("WaitingExpect should be set after action dispatch")
	}

	// Confirming event arrives → advance to terminal.
	confirm := domain.Event{ID: "ev-2", Kind: "android.window.state_changed", SeqNo: 2, OccurredAt: time.Now()}
	final, terminal2, err := eng.ProcessEvent(context.Background(), mid, "action-expect", task("t1"), confirm)
	if err != nil {
		t.Fatalf("unexpected error on confirm: %v", err)
	}
	if !terminal2 {
		t.Error("expected terminal=true after confirm event")
	}
	if !final.TerminalSuccess {
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
	mid, _, _ := eng.ProcessEvent(context.Background(), state, "timeout-test", task("t1"), anyEvent())
	if mid == nil || mid.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be armed")
	}

	// Force deadline to be in the past.
	mid.DeadlineAt = time.Now().Add(-1 * time.Second)

	// Next event arrives after deadline → handleFailure → retry (MaxRetry=1, RetryCount was 0).
	retry1, terminal, _ := eng.ProcessEvent(context.Background(), mid, "timeout-test", task("t1"), anyEvent())
	if terminal {
		t.Error("should not be terminal after first timeout (retry budget=1)")
	}
	if retry1.RetryCount != 1 {
		t.Errorf("expected RetryCount=1, got %d", retry1.RetryCount)
	}
	if retry1.WaitingExpect != nil {
		t.Error("WaitingExpect should be cleared after timeout")
	}

	// Arm expect again (retry fires on next event because trigger is empty).
	mid2, _, _ := eng.ProcessEvent(context.Background(), retry1, "timeout-test", task("t1"), anyEvent())
	if mid2 == nil || mid2.WaitingExpect == nil {
		t.Fatal("expected WaitingExpect to be re-armed on retry")
	}
	mid2.DeadlineAt = time.Now().Add(-1 * time.Second)

	// Second timeout — retry budget exhausted → OnFailure = terminal.
	final, terminal2, _ := eng.ProcessEvent(context.Background(), mid2, "timeout-test", task("t1"), anyEvent())
	if !terminal2 {
		t.Error("expected terminal after retry budget exhausted")
	}
	if final.TerminalSuccess {
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

	newState, terminal, err := eng.ProcessEvent(context.Background(), state, "tool-auto", task("t1"), anyEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !terminal {
		t.Error("expected terminal=true after tool call auto-execute")
	}
	if inv.calls != 1 {
		t.Errorf("expected 1 tool call, got %d", inv.calls)
	}
	if newState.Inputs["username"] != "Budi Santoso" {
		t.Errorf("expected username=Budi Santoso, got %q", newState.Inputs["username"])
	}
	if newState.Inputs["password"] != "S3cur3!" {
		t.Errorf("expected password=S3cur3!, got %q", newState.Inputs["password"])
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

	newState, terminal, err := eng.ProcessEvent(context.Background(), state, "optional-tool", task("t1"), anyEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !terminal {
		t.Error("expected terminal=true: optional tool failure should follow OnSuccess")
	}
	if !newState.TerminalSuccess {
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

	newState, terminal, err := eng.ProcessEvent(context.Background(), state, "required-tool", task("t1"), anyEvent())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !terminal {
		t.Error("expected terminal=true after tool failure")
	}
	if newState.TerminalSuccess {
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

	_, _, err := eng.ProcessEvent(context.Background(), state, "interp-tool", task("t1"), anyEvent())
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
	newState, _, err := eng.ProcessEvent(context.Background(), state, "specific-trigger", task("t1"),
		domain.Event{ID: "ev-1", Kind: "android.screen.changed", SeqNo: 1, OccurredAt: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newState != nil {
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
				Expect:    &domain.ExpectDef{Kind: "android.ui.observation"},
				Timeout:   "5s",
				OnSuccess: "click_next",
				OnFailure: "terminal",
			},
			// Empty trigger → auto-dispatched when reached via advance().
			"click_next": {
				Trigger: domain.EventMatch{},
				Action:  &domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "Next"}},
				Expect:  &domain.ExpectDef{Kind: "android.window.state_changed"},
				Timeout: "5s",
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
	eng := buildEngine(def, successDispatcher{})
	state := freshState("t1", "dev1")

	// First event: activates start → observe dispatched → WaitingExpect armed for "android.ui.observation".
	mid1, terminal, err := eng.ProcessEvent(context.Background(), state, "action-chain", task("t1"), anyEvent())
	if err != nil {
		t.Fatalf("step 1 error: %v", err)
	}
	if terminal {
		t.Error("should not be terminal after step 1 action")
	}
	if mid1.WaitingExpect == nil {
		t.Fatal("WaitingExpect should be armed after step 1 action")
	}
	if mid1.WaitingExpect.Kind != "android.ui.observation" {
		t.Errorf("wrong WaitingExpect kind: %q", mid1.WaitingExpect.Kind)
	}

	// Confirm event for start → advance to click_next → auto-execute → WaitingExpect armed for "android.window.state_changed".
	confirmObs := domain.Event{ID: "ev-2", Kind: "android.ui.observation", SeqNo: 2, OccurredAt: time.Now()}
	mid2, terminal, err := eng.ProcessEvent(context.Background(), mid1, "action-chain", task("t1"), confirmObs)
	if err != nil {
		t.Fatalf("step 2 confirm error: %v", err)
	}
	if terminal {
		t.Error("should not be terminal after auto-executing click_next")
	}
	if mid2.WaitingExpect == nil {
		t.Fatal("WaitingExpect should be armed after click_next auto-execute")
	}
	if mid2.WaitingExpect.Kind != "android.window.state_changed" {
		t.Errorf("wrong WaitingExpect kind: %q", mid2.WaitingExpect.Kind)
	}

	// Confirm event for click_next → terminal.
	confirmWin := domain.Event{ID: "ev-3", Kind: "android.window.state_changed", SeqNo: 3, OccurredAt: time.Now()}
	final, terminal, err := eng.ProcessEvent(context.Background(), mid2, "action-chain", task("t1"), confirmWin)
	if err != nil {
		t.Fatalf("step 3 confirm error: %v", err)
	}
	if !terminal {
		t.Error("expected terminal after final confirm")
	}
	if !final.TerminalSuccess {
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
