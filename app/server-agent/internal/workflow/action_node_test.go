package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// --- fakes ---

type fixedDispatcher struct {
	result domain.CommandResult
	err    error
}

func (d *fixedDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	if d.err != nil {
		return nil, d.err
	}
	ch := make(chan domain.CommandResult, 1)
	r := d.result
	r.CommandID = cmd.ID
	ch <- r
	close(ch)
	return ch, nil
}

// neverDispatcher returns a channel that never delivers — used to test ctx cancellation.
type neverDispatcher struct{}

func (neverDispatcher) Dispatch(_ context.Context, _ domain.Command) (<-chan domain.CommandResult, error) {
	return make(chan domain.CommandResult), nil
}

// --- helpers ---

func makeActionCmd(action *domain.ActionDef, expect *domain.ExpectDef, timeout string) NodeCommand {
	state := domain.NewWorkflowState(domain.TaskID("t1"), domain.DeviceID("dev1"))
	return NodeCommand{
		Step:  domain.StepDef{Action: action, Expect: expect, Timeout: timeout},
		State: state,
		Task:  &domain.Task{ID: domain.TaskID("t1"), Status: domain.TaskStatusRunning},
	}
}

// --- tests ---

// TestActionNode_Success_NoExpect: dispatch succeeds, no Expect → empty NodeOutput (advance).
func TestActionNode_Success_NoExpect(t *testing.T) {
	n := &ActionNode{disp: &fixedDispatcher{result: domain.CommandResult{Success: true}}}
	out, err := n.Execute(context.Background(), makeActionCmd(
		&domain.ActionDef{Kind: domain.ActionKindObserve}, nil, "",
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Errorf("unexpected business error: %v", out.Err)
	}
	if out.SuspendExpect != nil {
		t.Error("SuspendExpect should be nil when no Expect is set")
	}
}

// TestActionNode_Success_Expect_PreMatch: snapshot satisfies Expect → empty NodeOutput (advance without arming).
func TestActionNode_Success_Expect_PreMatch(t *testing.T) {
	raw := json.RawMessage(`{"snapshotAfter":{"packageName":"com.example","activityName":"com.example.Main","targets":[]}}`)
	n := &ActionNode{disp: &fixedDispatcher{result: domain.CommandResult{Success: true, Raw: raw}}}
	out, err := n.Execute(context.Background(), makeActionCmd(
		&domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
		&domain.ExpectDef{Package: "com.example", ClassSuffix: "Main"},
		"5s",
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Errorf("unexpected business error: %v", out.Err)
	}
	if out.SuspendExpect != nil {
		t.Error("SuspendExpect should be nil when snapshot pre-check matches")
	}
}

// TestActionNode_Success_Expect_NoPreMatch: snapshot does not satisfy Expect → SuspendExpect + Deadline set.
func TestActionNode_Success_Expect_NoPreMatch(t *testing.T) {
	raw := json.RawMessage(`{"snapshotAfter":{"packageName":"com.other","activityName":"com.other.Other","targets":[]}}`)
	n := &ActionNode{disp: &fixedDispatcher{result: domain.CommandResult{Success: true, Raw: raw}}}
	out, err := n.Execute(context.Background(), makeActionCmd(
		&domain.ActionDef{Kind: domain.ActionKindClick, Target: &domain.TargetDef{Kind: domain.TargetKindText, Value: "OK"}},
		&domain.ExpectDef{Package: "com.example", ClassSuffix: "Main"},
		"5s",
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Errorf("unexpected business error: %v", out.Err)
	}
	if out.SuspendExpect == nil {
		t.Fatal("SuspendExpect should be set when snapshot does not satisfy Expect")
	}
	if out.SuspendExpect.Package != "com.example" {
		t.Errorf("SuspendExpect.Package: want %q, got %q", "com.example", out.SuspendExpect.Package)
	}
	if out.Deadline != 5*time.Second {
		t.Errorf("Deadline: want 5s, got %v", out.Deadline)
	}
}

// TestActionNode_DispatchFailure: Dispatch returns error → NodeOutput.Err non-nil.
func TestActionNode_DispatchFailure(t *testing.T) {
	n := &ActionNode{disp: &fixedDispatcher{err: errors.New("conn refused")}}
	out, err := n.Execute(context.Background(), makeActionCmd(
		&domain.ActionDef{Kind: domain.ActionKindObserve}, nil, "",
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err == nil {
		t.Error("expected business error for dispatch failure")
	}
}

// TestActionNode_DeviceFailure: device returns Success=false → NodeOutput.Err non-nil.
func TestActionNode_DeviceFailure(t *testing.T) {
	n := &ActionNode{disp: &fixedDispatcher{result: domain.CommandResult{Success: false}}}
	out, err := n.Execute(context.Background(), makeActionCmd(
		&domain.ActionDef{Kind: domain.ActionKindObserve}, nil, "",
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err == nil {
		t.Error("expected business error for device failure")
	}
}

// TestActionNode_ContextCancelled: cancelled context returns system error (second return), not NodeOutput.Err.
func TestActionNode_ContextCancelled(t *testing.T) {
	n := &ActionNode{disp: neverDispatcher{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Execute

	out, err := n.Execute(ctx, makeActionCmd(
		&domain.ActionDef{Kind: domain.ActionKindObserve}, nil, "",
	))
	if out.Err != nil {
		t.Errorf("system cancel must not set NodeOutput.Err: %v", out.Err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled system error, got: %v", err)
	}
}
