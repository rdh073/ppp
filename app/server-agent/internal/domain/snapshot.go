package domain

import "time"

// UiSnapshot is the server-side model of the accessibility tree returned by
// the android-agent in response to device.observe or device.execute.
type UiSnapshot struct {
	ID           string
	DeviceID     DeviceID
	PackageName  string
	ActivityName string
	CapturedAt   time.Time
	Targets      []UiTarget
}

// UiTarget represents a single accessible node in the UI tree.
type UiTarget struct {
	TargetID   string
	Role       string
	Text       string
	ResourceID string
	Bounds     [4]int // [left, top, right, bottom]
	Actionable bool
	Enabled    bool
	Scrollable bool
	Focused    bool
}
