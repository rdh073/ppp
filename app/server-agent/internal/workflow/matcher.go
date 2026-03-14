package workflow

import (
	"encoding/json"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// deviceEventPayload is the enriched payload structure sent by the android-agent
// for android.* notification events. Fields are optional; missing fields are
// treated as empty strings.
type deviceEventPayload struct {
	EventType          string   `json:"eventType"`
	PackageName        string   `json:"packageName"`
	ClassName          string   `json:"className"`
	Text               []string `json:"text"`
	ContentDescription string   `json:"contentDescription"`
	ActiveWindowTitle  string   `json:"activeWindowTitle"`
}

func parsePayload(event domain.Event) deviceEventPayload {
	var p deviceEventPayload
	if raw := domain.MarshalEventPayload(event); len(raw) > 0 {
		_ = json.Unmarshal(raw, &p)
	}
	return p
}

// MatchEvent reports whether event satisfies m.
// All non-empty fields in m must match (AND semantics).
// Empty EventMatch{} matches any event.
func MatchEvent(m domain.EventMatch, event domain.Event) bool {
	if m.Kind != "" && m.Kind != event.Kind {
		return false
	}
	if m.Package == "" && m.ClassSuffix == "" && m.TextContains == "" {
		return true
	}
	p := parsePayload(event)
	if m.Package != "" && p.PackageName != m.Package {
		return false
	}
	if m.ClassSuffix != "" && !strings.HasSuffix(p.ClassName, m.ClassSuffix) {
		return false
	}
	if m.TextContains != "" && !containsText(p, m.TextContains) {
		return false
	}
	return true
}

// MatchExpect reports whether event satisfies the expect condition.
// Semantics identical to MatchEvent.
func MatchExpect(e domain.ExpectDef, event domain.Event) bool {
	if e.Kind != "" && e.Kind != event.Kind {
		return false
	}
	if e.Package == "" && e.ClassSuffix == "" && e.TextContains == "" {
		return true
	}
	p := parsePayload(event)
	if e.Package != "" && p.PackageName != e.Package {
		return false
	}
	if e.ClassSuffix != "" && !strings.HasSuffix(p.ClassName, e.ClassSuffix) {
		return false
	}
	if e.TextContains != "" && !containsText(p, e.TextContains) {
		return false
	}
	return true
}

func containsText(p deviceEventPayload, substr string) bool {
	for _, t := range p.Text {
		if strings.Contains(t, substr) {
			return true
		}
	}
	if strings.Contains(p.ContentDescription, substr) {
		return true
	}
	if strings.Contains(p.ActiveWindowTitle, substr) {
		return true
	}
	return false
}
