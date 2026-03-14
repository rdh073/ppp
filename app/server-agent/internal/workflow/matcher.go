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
	return matchFields(event, m.Kind, m.Package, m.ClassSuffix, m.TextContains)
}

// MatchExpect reports whether event satisfies the expect condition.
// Semantics identical to MatchEvent.
func MatchExpect(e domain.ExpectDef, event domain.Event) bool {
	return matchFields(event, e.Kind, e.Package, e.ClassSuffix, e.TextContains)
}

// matchFields is the shared AND-matching kernel used by both MatchEvent and MatchExpect.
func matchFields(event domain.Event, kind domain.EventKind, pkg, classSuffix, textContains string) bool {
	if kind != "" && kind != event.Kind {
		return false
	}
	if pkg == "" && classSuffix == "" && textContains == "" {
		return true
	}
	p := parsePayload(event)
	if pkg != "" && p.PackageName != pkg {
		return false
	}
	if classSuffix != "" && !strings.HasSuffix(p.ClassName, classSuffix) {
		return false
	}
	if textContains != "" && !containsText(p, textContains) {
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
