package domain

import "time"

// UiSnapshot is the server-side model of the accessibility tree returned by
// the android-agent in response to device.observe or device.execute.
type UiSnapshot struct {
	ID           string     `json:"snapshotId"`
	DeviceID     DeviceID   `json:"deviceId"`
	PackageName  string     `json:"packageName,omitempty"`
	ActivityName string     `json:"activityName,omitempty"`
	ScreenState  string     `json:"screenState,omitempty"`
	CapturedAt   time.Time  `json:"capturedAt"`
	Targets      []UiTarget `json:"targets"`
}

// UiTarget represents a single accessible node in the UI tree.
type UiTarget struct {
	TargetID    string `json:"targetId"`
	Role        string `json:"role,omitempty"`
	Text        string `json:"text,omitempty"`
	ResourceID  string `json:"resourceId,omitempty"`
	PackageName string `json:"packageName,omitempty"`
	Bounds      [4]int `json:"bounds"` // [left, top, right, bottom]
	Actionable  bool   `json:"actionable"`
	Enabled     bool   `json:"enabled"`
	Checked     *bool  `json:"checked,omitempty"`
	Selected    bool   `json:"selected"`
	Scrollable  bool   `json:"scrollable"`
	Focused     bool   `json:"focused"`
	Password    bool   `json:"password"`
}
