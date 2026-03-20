package workflow_test

import (
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

func validDef() *domain.WorkflowDef {
	return &domain.WorkflowDef{
		Name:  "test",
		Entry: "start",
		Steps: map[string]domain.StepDef{
			"start": {
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	}
}

func TestValidate_ValidDef(t *testing.T) {
	if err := workflow.Validate(validDef()); err != nil {
		t.Fatalf("expected valid def to pass, got: %v", err)
	}
}

func TestValidate_EmptyEntry(t *testing.T) {
	def := validDef()
	def.Entry = ""
	if err := workflow.Validate(def); err == nil {
		t.Fatal("expected error for empty entry")
	}
}

func TestValidate_UnknownEntry(t *testing.T) {
	def := validDef()
	def.Entry = "nonexistent"
	if err := workflow.Validate(def); err == nil {
		t.Fatal("expected error for unknown entry step")
	}
}

func TestValidate_UnknownOnSuccess(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		OnSuccess: "ghost_step",
		OnFailure: "terminal",
	}
	if err := workflow.Validate(def); err == nil {
		t.Fatal("expected error for unknown on_success step")
	}
}

func TestValidate_UnknownOnFailure(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		OnSuccess: "terminal",
		OnFailure: "ghost_step",
	}
	if err := workflow.Validate(def); err == nil {
		t.Fatal("expected error for unknown on_failure step")
	}
}

func TestValidate_BothActionAndToolCall(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Action:    &domain.ActionDef{Kind: domain.ActionKindObserve},
		ToolCall:  &domain.ToolCallDef{ToolName: "some.tool"},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	if err := workflow.Validate(def); err == nil {
		t.Fatal("expected error when both action and tool_call are set")
	}
}

func TestValidate_MultiStep_AllValid(t *testing.T) {
	def := &domain.WorkflowDef{
		Name:  "multi",
		Entry: "step1",
		Steps: map[string]domain.StepDef{
			"step1": {OnSuccess: "step2", OnFailure: "terminal"},
			"step2": {OnSuccess: "terminal", OnFailure: "step1"},
		},
	}
	if err := workflow.Validate(def); err != nil {
		t.Fatalf("expected valid multi-step def to pass, got: %v", err)
	}
}

func TestValidate_RejectsLegacyAndroidWindowStateChanged(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Trigger:   domain.EventMatch{Kind: "android.window.state_changed"},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}

	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected legacy event kind to be rejected")
	}
	if !strings.Contains(err.Error(), string(domain.EventKindScreenChanged)) {
		t.Fatalf("expected replacement hint for %q, got: %v", domain.EventKindScreenChanged, err)
	}
}

func TestValidate_RejectsLegacyUiObservationExpect(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Action:    &domain.ActionDef{Kind: domain.ActionKindObserve},
		Expect:    &domain.ExpectDef{Kind: "android.ui.observation"},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}

	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected legacy observe expect kind to be rejected")
	}
	if !strings.Contains(err.Error(), string(domain.EventKindScreenChanged)) {
		t.Fatalf("expected replacement hint for %q, got: %v", domain.EventKindScreenChanged, err)
	}
}

func TestValidate_AllowsSemanticKeyAndUiMatchers(t *testing.T) {
	ready := true
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Trigger: domain.EventMatch{
			Kind: domain.EventKindScreenChanged,
			UI: &domain.UiMatch{
				ActiveUIKey:   "login.ready",
				UIReady:       &ready,
				ButtonKey:     "login.submit",
				ButtonEnabled: &ready,
			},
		},
		Action: &domain.ActionDef{
			Kind: domain.ActionKindClick,
			Target: &domain.TargetDef{
				Kind:  domain.TargetKindSemanticKey,
				Value: "login.submit",
			},
		},
		Expect: &domain.ExpectDef{
			Kind: domain.EventKindScreenChanged,
			UI: &domain.UiMatch{
				ActiveUIKey: "home.ready",
				UIReady:     &ready,
			},
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}

	if err := workflow.Validate(def); err != nil {
		t.Fatalf("expected semantic workflow def to pass, got: %v", err)
	}
}

func TestValidate_RejectsUnknownTargetKind(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Action: &domain.ActionDef{
			Kind: domain.ActionKindClick,
			Target: &domain.TargetDef{
				Kind:  domain.TargetKind("mystery"),
				Value: "x",
			},
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}

	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected invalid target kind to be rejected")
	}
	if !strings.Contains(err.Error(), "action.target kind") {
		t.Fatalf("expected target kind error, got: %v", err)
	}
}

func TestValidate_InvalidTimeoutString(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Timeout:   "not-a-duration",
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected error for invalid timeout string")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout in error message, got: %v", err)
	}
}

func TestValidate_ValidTimeoutString(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Timeout:   "30s",
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	if err := workflow.Validate(def); err != nil {
		t.Fatalf("expected valid timeout to pass, got: %v", err)
	}
}

func TestValidate_OpenIntentRequiresIntentAction(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Action: &domain.ActionDef{
			Kind: domain.ActionKindOpenIntent,
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected error for missing intent_action")
	}
	if !strings.Contains(err.Error(), "intent_action") {
		t.Fatalf("expected intent_action validation error, got: %v", err)
	}
}

// --- OR expect validation tests ---

func TestValidate_ExpectOr_Valid(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Action: &domain.ActionDef{Kind: domain.ActionKindObserve},
		Expect: &domain.ExpectDef{
			Or: []domain.ExpectDef{
				{Kind: domain.EventKindScreenChanged, TextContains: "Saved"},
				{Kind: domain.EventKindNotification, TextContains: "Berhasil"},
			},
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	if err := workflow.Validate(def); err != nil {
		t.Fatalf("expected valid OR expect to pass, got: %v", err)
	}
}

func TestValidate_ExpectOr_MutuallyExclusiveWithKind(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Expect: &domain.ExpectDef{
			Kind: domain.EventKindScreenChanged, // conflict with Or
			Or: []domain.ExpectDef{
				{Kind: domain.EventKindScreenChanged},
				{Kind: domain.EventKindNotification},
			},
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected error: or + kind are mutually exclusive")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

func TestValidate_ExpectOr_RequiresAtLeastTwoClauses(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Expect: &domain.ExpectDef{
			Or: []domain.ExpectDef{
				{Kind: domain.EventKindScreenChanged},
			},
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected error: or requires at least 2 clauses")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("expected 'at least 2' error, got: %v", err)
	}
}

func TestValidate_ExpectOr_RejectsNestedOr(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Expect: &domain.ExpectDef{
			Or: []domain.ExpectDef{
				{Kind: domain.EventKindScreenChanged},
				{
					Or: []domain.ExpectDef{ // nested — not supported
						{Kind: domain.EventKindNotification},
						{Kind: domain.EventKindScreenChanged},
					},
				},
			},
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected error: nested or is not supported")
	}
	if !strings.Contains(err.Error(), "nested") {
		t.Fatalf("expected nested error, got: %v", err)
	}
}

func TestValidate_ExpectOr_RejectsLegacyKindInClause(t *testing.T) {
	def := validDef()
	def.Steps["start"] = domain.StepDef{
		Expect: &domain.ExpectDef{
			Or: []domain.ExpectDef{
				{Kind: "android.window.state_changed"}, // legacy
				{Kind: domain.EventKindNotification},
			},
		},
		OnSuccess: "terminal",
		OnFailure: "terminal",
	}
	err := workflow.Validate(def)
	if err == nil {
		t.Fatal("expected error: legacy kind in or clause")
	}
	if !strings.Contains(err.Error(), string(domain.EventKindScreenChanged)) {
		t.Fatalf("expected replacement hint, got: %v", err)
	}
}
