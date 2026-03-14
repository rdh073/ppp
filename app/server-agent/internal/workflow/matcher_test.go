package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

func makeEvent(kind domain.EventKind, payload string) domain.Event {
	return domain.Event{
		Kind:    kind,
		Payload: json.RawMessage(payload),
	}
}

const androidKind domain.EventKind = "android.window.state_changed"
const otherKind domain.EventKind = "android.screen.changed"

// TestMatchEvent_EmptyMatchesAll verifies that an empty EventMatch matches any event.
func TestMatchEvent_EmptyMatchesAll(t *testing.T) {
	event := makeEvent(androidKind, `{
		"packageName": "com.android.settings",
		"className": "com.android.settings.network.PrivateDnsSettings",
		"text": ["Private DNS provider hostname"]
	}`)
	if !workflow.MatchEvent(domain.EventMatch{}, event) {
		t.Error("empty EventMatch should match any event")
	}
}

// TestMatchEvent_KindMismatch verifies that a Kind filter rejects non-matching events.
func TestMatchEvent_KindMismatch(t *testing.T) {
	event := makeEvent(otherKind, `{}`)
	m := domain.EventMatch{Kind: androidKind}
	if workflow.MatchEvent(m, event) {
		t.Error("EventMatch with different Kind should not match")
	}
}

// TestMatchEvent_KindMatch verifies that a matching Kind passes the filter.
func TestMatchEvent_KindMatch(t *testing.T) {
	event := makeEvent(androidKind, `{}`)
	m := domain.EventMatch{Kind: androidKind}
	if !workflow.MatchEvent(m, event) {
		t.Error("EventMatch with same Kind should match")
	}
}

// TestMatchEvent_PackageMatch verifies that a matching packageName passes the filter.
func TestMatchEvent_PackageMatch(t *testing.T) {
	event := makeEvent(androidKind, `{"packageName":"com.android.settings"}`)
	m := domain.EventMatch{Kind: androidKind, Package: "com.android.settings"}
	if !workflow.MatchEvent(m, event) {
		t.Error("EventMatch with matching Package should match")
	}
}

// TestMatchEvent_PackageMismatch verifies that a wrong packageName rejects the event.
func TestMatchEvent_PackageMismatch(t *testing.T) {
	event := makeEvent(androidKind, `{"packageName":"com.example.other"}`)
	m := domain.EventMatch{Kind: androidKind, Package: "com.android.settings"}
	if workflow.MatchEvent(m, event) {
		t.Error("EventMatch with mismatched Package should not match")
	}
}

// TestMatchEvent_ClassSuffix verifies that a className ending with ClassSuffix passes.
func TestMatchEvent_ClassSuffix(t *testing.T) {
	event := makeEvent(androidKind, `{
		"packageName": "com.android.settings",
		"className": "com.android.settings.network.PrivateDnsSettings"
	}`)
	m := domain.EventMatch{
		Kind:        androidKind,
		Package:     "com.android.settings",
		ClassSuffix: "PrivateDnsSettings",
	}
	if !workflow.MatchEvent(m, event) {
		t.Error("EventMatch with matching ClassSuffix should match")
	}

	// Wrong suffix should not match.
	m2 := domain.EventMatch{
		Kind:        androidKind,
		Package:     "com.android.settings",
		ClassSuffix: "WifiSettings",
	}
	if workflow.MatchEvent(m2, event) {
		t.Error("EventMatch with non-matching ClassSuffix should not match")
	}
}

// TestMatchEvent_TextContains verifies that a text array containing the substring matches.
func TestMatchEvent_TextContains(t *testing.T) {
	event := makeEvent(androidKind, `{
		"packageName": "com.android.settings",
		"className": "com.android.settings.network.PrivateDnsSettings",
		"text": ["Private DNS provider hostname", "Enter hostname"]
	}`)
	m := domain.EventMatch{
		Kind:         androidKind,
		Package:      "com.android.settings",
		TextContains: "Private DNS",
	}
	if !workflow.MatchEvent(m, event) {
		t.Error("EventMatch with matching TextContains should match")
	}

	m2 := domain.EventMatch{
		Kind:         androidKind,
		Package:      "com.android.settings",
		TextContains: "Bluetooth",
	}
	if workflow.MatchEvent(m2, event) {
		t.Error("EventMatch with non-matching TextContains should not match")
	}
}

// TestMatchExpect_Basic verifies MatchExpect has identical semantics to MatchEvent.
func TestMatchExpect_Basic(t *testing.T) {
	event := makeEvent(androidKind, `{
		"packageName": "com.android.settings",
		"className": "com.android.settings.network.PrivateDnsSettings",
		"text": ["Private DNS provider hostname"],
		"activeWindowTitle": "Private DNS"
	}`)

	// Empty ExpectDef matches any.
	if !workflow.MatchExpect(domain.ExpectDef{}, event) {
		t.Error("empty ExpectDef should match any event")
	}

	// Kind mismatch.
	if workflow.MatchExpect(domain.ExpectDef{Kind: otherKind}, event) {
		t.Error("ExpectDef with different Kind should not match")
	}

	// Full match.
	e := domain.ExpectDef{
		Kind:         androidKind,
		Package:      "com.android.settings",
		ClassSuffix:  "PrivateDnsSettings",
		TextContains: "Private DNS",
	}
	if !workflow.MatchExpect(e, event) {
		t.Error("ExpectDef with all matching fields should match")
	}

	// TextContains from activeWindowTitle.
	e2 := domain.ExpectDef{
		Kind:         androidKind,
		TextContains: "Private DNS",
	}
	if !workflow.MatchExpect(e2, event) {
		t.Error("ExpectDef should match text found in activeWindowTitle")
	}
}
