package domain

import "time"

// UiSnapshot is the server-side model of the accessibility tree returned by
// the android-agent in response to device.observe or device.execute.
type UiSnapshot struct {
	ID              string           `json:"snapshotId"`
	DeviceID        DeviceID         `json:"deviceId"`
	PackageName     string           `json:"packageName,omitempty"`
	ActivityName    string           `json:"activityName,omitempty"`
	ScreenState     string           `json:"screenState,omitempty"`
	FocusedTargetID string           `json:"focusedTargetId,omitempty"`
	CapturedAt      time.Time        `json:"capturedAt"`
	Targets         []UiTarget       `json:"targets"`
	Semantic        *UiSemanticState `json:"semantic,omitempty"`
}

// UiTarget represents a single accessible node in the UI tree.
type UiTarget struct {
	TargetID    string `json:"targetId"`
	Role        string `json:"role,omitempty"`
	UIRole      string `json:"uiRole,omitempty"`
	Label       string `json:"label,omitempty"`
	Text        string `json:"text,omitempty"`
	ResourceID  string `json:"resourceId,omitempty"`
	PackageName string `json:"packageName,omitempty"`
	SemanticKey string `json:"semanticKey,omitempty"`
	FormKey     string `json:"formKey,omitempty"`
	Bounds      [4]int `json:"bounds"` // [left, top, right, bottom]
	Actionable  bool   `json:"actionable"`
	Enabled     bool   `json:"enabled"`
	Checked     *bool  `json:"checked,omitempty"`
	Selected    bool   `json:"selected"`
	Scrollable  bool   `json:"scrollable"`
	Focused     bool   `json:"focused"`
	Password    bool   `json:"password"`
}

// UiSemanticState is the normalized semantic view of the active Android UI.
type UiSemanticState struct {
	ActiveUIKey      string          `json:"activeUiKey,omitempty"`
	BaseScreenKey    string          `json:"baseScreenKey,omitempty"`
	OverlayKey       string          `json:"overlayKey,omitempty"`
	UIReady          bool            `json:"uiReady"`
	SemanticDigest   string          `json:"semanticDigest,omitempty"`
	FocusedTargetKey string          `json:"focusedTargetKey,omitempty"`
	Forms            []UiFormState   `json:"forms,omitempty"`
	Buttons          []UiButtonState `json:"buttons,omitempty"`
}

// UiFormState describes a semantically grouped form on screen.
type UiFormState struct {
	FormKey         string   `json:"formKey,omitempty"`
	FieldKeys       []string `json:"fieldKeys,omitempty"`
	FocusedFieldKey string   `json:"focusedFieldKey,omitempty"`
	Ready           bool     `json:"ready"`
}

// UiButtonState describes a semantically classified button on screen.
type UiButtonState struct {
	ButtonKey string `json:"buttonKey,omitempty"`
	Enabled   bool   `json:"enabled"`
	Visible   bool   `json:"visible"`
	Primary   bool   `json:"primary"`
}
