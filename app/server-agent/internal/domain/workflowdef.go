package domain

// WorkflowDef is a YAML-serialisable step graph.
// Each step declares: what event activates it (Trigger), what typed command
// to issue (Action), what event confirms success (Expect), and where to route
// on success or failure (OnSuccess / OnFailure).
//
// The engine evaluates steps against incoming device events without any
// artifact side-channel. Routing is data-driven from the def, not from
// Go node handlers.
type WorkflowDef struct {
	Name    string             `yaml:"name"    json:"name"`
	Version int                `yaml:"version" json:"version"`
	Entry   string             `yaml:"entry"   json:"entry"` // step id
	Steps   map[string]StepDef `yaml:"steps"   json:"steps"`
}

// StepDef is one node in the workflow graph.
//
//	Trigger   — which event activates this step (matched against event payload).
//	            Empty Trigger matches any event; if reached via advance(), the step
//	            is auto-executed immediately without waiting for a new device event.
//	Action    — typed device command. Nil = routing or tool-call-only step.
//	ToolCall  — synchronous tool invocation. Outputs are merged into WorkflowState.Inputs.
//	            A step may have Action or ToolCall, not both.
//	Expect    — event that confirms the action succeeded. Nil = advance immediately.
//	OnSuccess — step id to advance to on success, or "terminal".
//	OnFailure — step id to advance to on failure, or "terminal".
//	Timeout   — how long to wait for the Expect event (e.g. "5s"). Default 10s.
//	MaxRetry  — how many times to retry before following OnFailure.
type StepDef struct {
	Trigger   EventMatch   `yaml:"trigger"              json:"trigger"`
	Action    *ActionDef   `yaml:"action,omitempty"     json:"action,omitempty"`
	ToolCall  *ToolCallDef `yaml:"tool_call,omitempty"  json:"tool_call,omitempty"`
	Expect    *ExpectDef   `yaml:"expect,omitempty"     json:"expect,omitempty"`
	OnSuccess string       `yaml:"on_success"           json:"on_success"`
	OnFailure string       `yaml:"on_failure"           json:"on_failure"`
	Timeout   string       `yaml:"timeout,omitempty"    json:"timeout,omitempty"`
	MaxRetry  int          `yaml:"max_retry,omitempty"  json:"max_retry,omitempty"`
}

// ToolCallDef invokes a registered tool synchronously when a step is activated.
// String values in Params support {{input.key}} interpolation from WorkflowState.Inputs.
// Outputs maps top-level JSON string keys in the tool result to WorkflowState.Inputs keys.
//
// Example (YAML):
//
//	tool_call:
//	  tool_name: identity.generate_indonesian_name
//	  params:
//	    gender: "{{input.gender}}"
//	  outputs:
//	    fullName: username_full
//	  optional: false
type ToolCallDef struct {
	ToolName string            `yaml:"tool_name"          json:"tool_name"`
	Params   map[string]string `yaml:"params,omitempty"   json:"params,omitempty"`
	// Outputs maps top-level JSON keys in the tool result to WorkflowState.Inputs keys.
	Outputs map[string]string `yaml:"outputs,omitempty"  json:"outputs,omitempty"`
	// Optional: if true, a tool invocation failure is silently skipped (OnSuccess path).
	Optional bool `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// EventMatch selects which device events activate a step.
// All non-empty fields must match (AND semantics).
// Empty struct matches any event.
type EventMatch struct {
	Kind         EventKind `yaml:"kind,omitempty"           json:"kind,omitempty"`
	Package      string    `yaml:"package,omitempty"        json:"package,omitempty"`
	ClassSuffix  string    `yaml:"class_suffix,omitempty"   json:"class_suffix,omitempty"`
	TextContains string    `yaml:"text_contains,omitempty"  json:"text_contains,omitempty"`
	UI           *UiMatch  `yaml:"ui,omitempty"             json:"ui,omitempty"`
}

// IsEmpty reports whether all filtering criteria are unset.
// An empty EventMatch matches any event; the engine auto-executes a step
// with an empty trigger immediately when it is reached via advance().
func (m EventMatch) IsEmpty() bool {
	return m.Kind == "" &&
		m.Package == "" &&
		m.ClassSuffix == "" &&
		m.TextContains == "" &&
		(m.UI == nil || m.UI.IsEmpty())
}

// ActionDef is a typed command to be sent to the device via device.execute.
// String values support {{input.key}} interpolation from WorkflowState.Inputs.
type ActionDef struct {
	Kind      ActionKind   `yaml:"kind"                    json:"kind"`
	Target    *TargetDef   `yaml:"target,omitempty"        json:"target,omitempty"`
	InputText string       `yaml:"input_text,omitempty"    json:"input_text,omitempty"` // for input_text
	Package   string       `yaml:"package,omitempty"       json:"package,omitempty"`    // for open_app
	Direction string       `yaml:"direction,omitempty"     json:"direction,omitempty"`  // for scroll: up|down|left|right
	Fields    []FieldEntry `yaml:"fields,omitempty"        json:"fields,omitempty"`     // for fill_form
}

// FieldEntry is one input field in a fill_form action.
// Target identifies the UI element; Value is the text to type.
// Both support {{input.key}} interpolation from WorkflowState.Inputs.
type FieldEntry struct {
	Target TargetDef `yaml:"target" json:"target"`
	Value  string    `yaml:"value"  json:"value"`
}

// TargetDef identifies which UI element to interact with.
// Value supports {{input.key}} interpolation.
type TargetDef struct {
	Kind  TargetKind `yaml:"kind"  json:"kind"`
	Value string     `yaml:"value" json:"value"`
}

// ExpectDef describes the device event that confirms an action succeeded.
// All non-empty fields must match (AND semantics).
type ExpectDef struct {
	Kind         EventKind `yaml:"kind,omitempty"           json:"kind,omitempty"`
	Package      string    `yaml:"package,omitempty"        json:"package,omitempty"`
	ClassSuffix  string    `yaml:"class_suffix,omitempty"   json:"class_suffix,omitempty"`
	TextContains string    `yaml:"text_contains,omitempty"  json:"text_contains,omitempty"`
	UI           *UiMatch  `yaml:"ui,omitempty"             json:"ui,omitempty"`
}

// UiMatch describes semantic UI facts to match against event payloads or
// snapshotAfter payloads. All non-empty fields must match (AND semantics).
type UiMatch struct {
	ActiveUIKey      string `yaml:"active_ui_key,omitempty"      json:"active_ui_key,omitempty"`
	BaseScreenKey    string `yaml:"base_screen_key,omitempty"    json:"base_screen_key,omitempty"`
	OverlayKey       string `yaml:"overlay_key,omitempty"        json:"overlay_key,omitempty"`
	UIReady          *bool  `yaml:"ui_ready,omitempty"           json:"ui_ready,omitempty"`
	FormKey          string `yaml:"form_key,omitempty"           json:"form_key,omitempty"`
	FormReady        *bool  `yaml:"form_ready,omitempty"         json:"form_ready,omitempty"`
	ButtonKey        string `yaml:"button_key,omitempty"         json:"button_key,omitempty"`
	ButtonEnabled    *bool  `yaml:"button_enabled,omitempty"     json:"button_enabled,omitempty"`
	FocusedTargetKey string `yaml:"focused_target_key,omitempty" json:"focused_target_key,omitempty"`
}

// IsEmpty reports whether all semantic UI criteria are unset.
func (m UiMatch) IsEmpty() bool {
	return m.ActiveUIKey == "" &&
		m.BaseScreenKey == "" &&
		m.OverlayKey == "" &&
		m.UIReady == nil &&
		m.FormKey == "" &&
		m.FormReady == nil &&
		m.ButtonKey == "" &&
		m.ButtonEnabled == nil &&
		m.FocusedTargetKey == ""
}

// ActionKind is the type of UI command.
type ActionKind string

const (
	ActionKindOpenApp   ActionKind = "open_app"
	ActionKindClick     ActionKind = "click"
	ActionKindLongClick ActionKind = "long_click"
	ActionKindInputText ActionKind = "input_text"
	ActionKindScroll    ActionKind = "scroll"
	ActionKindObserve   ActionKind = "observe"   // explicit snapshot fetch, used as fallback
	ActionKindFillForm  ActionKind = "fill_form" // fill multiple input fields in one device round-trip
)

// TargetKind selects the accessibility attribute used to identify a UI element.
type TargetKind string

const (
	TargetKindText               TargetKind = "text"
	TargetKindResourceID         TargetKind = "resource_id"
	TargetKindContentDescription TargetKind = "content_description"
	TargetKindSemanticKey        TargetKind = "semantic_key"
	TargetKindClass              TargetKind = "class"
	TargetKindCoordinate         TargetKind = "coordinate" // Value: "x,y" (pixels). Empty = no-op.
)
