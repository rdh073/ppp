package domain

import "time"

// WorkflowState is the per-device, per-task mutable execution state.
// Checkpointed to the store after every step transition.
//
// CurrentStep is the step id in WorkflowDef (not a NodeKind).
// Inputs holds task-level parameters (e.g. private_dns_hostname) for
// {{input.key}} interpolation in ActionDef and TargetDef values.
// WaitingExpect is non-nil when the engine has issued an action and is
// suspended waiting for a confirming event. Cleared once the event matches.
// RetryCount tracks consecutive retries for the current step only;
// reset to 0 on every successful step transition.
type WorkflowState struct {
	TaskID        TaskID
	DeviceID      DeviceID
	Revision      uint64     // optimistic concurrency token
	CurrentStep     string     // step id in WorkflowDef; "terminal" = done
	RetryCount      int        // retries for current step
	TerminalSuccess bool       // set by engine when terminal reached via success path
	WaitingExpect   *ExpectDef // non-nil = suspended, waiting for confirming event
	DeadlineAt    time.Time  // zero = no deadline; set when WaitingExpect is armed
	Inputs        map[string]string
	UpdatedAt     time.Time
}

func NewWorkflowState(taskID TaskID, deviceID DeviceID) *WorkflowState {
	return &WorkflowState{
		TaskID:    taskID,
		DeviceID:  deviceID,
		Inputs:    make(map[string]string),
		UpdatedAt: time.Now(),
	}
}

// NewBootstrapWorkflowState seeds a fresh checkpoint from task-scoped inputs.
func NewBootstrapWorkflowState(task *Task, deviceID DeviceID) *WorkflowState {
	state := NewWorkflowState(task.ID, deviceID)
	for k, v := range task.InputArtifacts {
		state.Inputs[k] = v
	}
	return state
}

// IsTerminal reports whether this state has reached a terminal step.
func (s *WorkflowState) IsTerminal() bool {
	return s.CurrentStep == "terminal"
}
