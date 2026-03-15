package workflow

import (
	"errors"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// Validate checks a WorkflowDef for structural correctness.
// Returns nil when the def is valid, or a joined error listing all problems.
func Validate(def *domain.WorkflowDef) error {
	var errs []error

	// (a) Entry step must exist.
	if def.Entry == "" {
		errs = append(errs, fmt.Errorf("entry step is empty"))
	} else if _, ok := def.Steps[def.Entry]; !ok {
		errs = append(errs, fmt.Errorf("entry step %q not found in steps", def.Entry))
	}

	// (b) All on_success / on_failure values must be "terminal" or an existing step ID.
	for id, step := range def.Steps {
		if !isValidTarget(def, step.OnSuccess) {
			errs = append(errs, fmt.Errorf("step %q: on_success %q is not a valid step or \"terminal\"", id, step.OnSuccess))
		}
		if !isValidTarget(def, step.OnFailure) {
			errs = append(errs, fmt.Errorf("step %q: on_failure %q is not a valid step or \"terminal\"", id, step.OnFailure))
		}
		// (c) A step must not have both Action and ToolCall set.
		if step.Action != nil && step.ToolCall != nil {
			errs = append(errs, fmt.Errorf("step %q: action and tool_call are mutually exclusive", id))
		}
		errs = append(errs, validateStep(id, step)...)
	}

	return errors.Join(errs...)
}

func isValidTarget(def *domain.WorkflowDef, target string) bool {
	if target == "terminal" {
		return true
	}
	_, ok := def.Steps[target]
	return ok
}

func validateStep(stepID string, step domain.StepDef) []error {
	var errs []error

	if err := validateEventKind(stepID, "trigger", step.Trigger.Kind); err != nil {
		errs = append(errs, err)
	}
	if step.Expect != nil {
		if err := validateEventKind(stepID, "expect", step.Expect.Kind); err != nil {
			errs = append(errs, err)
		}
	}
	if step.Timeout != "" {
		if _, err := time.ParseDuration(step.Timeout); err != nil {
			errs = append(errs, fmt.Errorf("step %q: timeout %q: %w", stepID, step.Timeout, err))
		}
	}
	if step.Action != nil {
		errs = append(errs, validateAction(stepID, *step.Action)...)
	}

	return errs
}

func validateEventKind(
	stepID string,
	field string,
	kind domain.EventKind,
) error {
	switch kind {
	case "":
		return nil
	case "android.window.state_changed", "android.ui.observation", "ui.observation":
		return fmt.Errorf("step %q: %s kind %q is legacy; use %q for device UI transitions", stepID, field, kind, domain.EventKindScreenChanged)
	default:
		return nil
	}
}

func validateAction(
	stepID string,
	action domain.ActionDef,
) []error {
	var errs []error

	switch action.Kind {
	case domain.ActionKindOpenApp,
		domain.ActionKindClick,
		domain.ActionKindLongClick,
		domain.ActionKindInputText,
		domain.ActionKindScroll,
		domain.ActionKindObserve,
		domain.ActionKindFillForm:
	default:
		return []error{fmt.Errorf("step %q: unsupported action kind %q", stepID, action.Kind)}
	}

	switch action.Kind {
	case domain.ActionKindClick, domain.ActionKindLongClick, domain.ActionKindInputText:
		if action.Target == nil {
			errs = append(errs, fmt.Errorf("step %q: action kind %q requires target", stepID, action.Kind))
		}
	case domain.ActionKindFillForm:
		if len(action.Fields) == 0 {
			errs = append(errs, fmt.Errorf("step %q: fill_form requires at least one field", stepID))
		}
	}

	if action.Target != nil {
		if err := validateTarget(stepID, "action.target", *action.Target); err != nil {
			errs = append(errs, err)
		}
	}
	for i, field := range action.Fields {
		if err := validateTarget(stepID, fmt.Sprintf("action.fields[%d].target", i), field.Target); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

func validateTarget(
	stepID string,
	path string,
	target domain.TargetDef,
) error {
	switch target.Kind {
	case domain.TargetKindText,
		domain.TargetKindResourceID,
		domain.TargetKindContentDescription,
		domain.TargetKindSemanticKey,
		domain.TargetKindClass,
		domain.TargetKindCoordinate:
		return nil
	default:
		return fmt.Errorf("step %q: %s kind %q is unsupported", stepID, path, target.Kind)
	}
}
