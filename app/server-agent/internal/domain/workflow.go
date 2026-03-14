package domain

import "time"

type NodeKind string

const (
	NodeKindObserve  NodeKind = "observe"
	NodeKindDecide   NodeKind = "decide"
	NodeKindAct      NodeKind = "act"
	NodeKindVerify   NodeKind = "verify"
	NodeKindResync   NodeKind = "resync"
	NodeKindToolCall NodeKind = "toolcall"
	NodeKindWait     NodeKind = "wait"
	NodeKindTerminal NodeKind = "terminal"
)

// WorkflowState is the per-device, per-task mutable execution state.
// It is checkpointed to the store after every node transition.
// Artifacts is the inter-node key-value store; all values are strings so
// they survive JSON serialisation without a schema change.
type WorkflowState struct {
	TaskID         TaskID
	DeviceID       DeviceID
	Revision       uint64 // optimistic concurrency token for checkpoint advancement
	CurrentNode    NodeKind
	AttemptCount   int    // consecutive attempts at the current node
	ErrorCount     int    // consecutive errors, used for stop-condition
	LastSnapshotID string // most-recent snapshot ID processed
	Artifacts      map[string]string
	// WaitingFor lists the event kinds that must arrive before the workflow
	// advances again. When non-nil, ProcessEvent will skip the node unless
	// the incoming event matches one of these kinds.
	// Cleared automatically once a matching event is processed.
	WaitingFor []EventKind
	UpdatedAt  time.Time
}

func NewWorkflowState(taskID TaskID, deviceID DeviceID) *WorkflowState {
	return &WorkflowState{
		TaskID:      taskID,
		DeviceID:    deviceID,
		CurrentNode: NodeKindObserve,
		Artifacts:   make(map[string]string),
		UpdatedAt:   time.Now(),
	}
}
