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

// --- SnapshotMatchesExpect tests ---

const snapshotRaw = `{
	"snapshotAfter": {
		"packageName": "com.android.settings",
		"activityName": "com.android.settings.network.PrivateDnsSettings",
		"targets": [
			{"text": "Private DNS provider hostname"},
			{"text": "Save"}
		]
	}
}`

// TestSnapshotMatchesExpect_KindOnly returns false because Kind is not a
// state property derivable from a snapshot.
func TestSnapshotMatchesExpect_KindOnly(t *testing.T) {
	exp := domain.ExpectDef{Kind: "android.activity.created"}
	if workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("Kind-only ExpectDef should return false (Kind is not a snapshot property)")
	}
}

// TestSnapshotMatchesExpect_Empty returns false when no state fields are set.
func TestSnapshotMatchesExpect_Empty(t *testing.T) {
	if workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), domain.ExpectDef{}) {
		t.Error("empty ExpectDef should return false")
	}
}

// TestSnapshotMatchesExpect_PackageMatch returns true on exact package match.
func TestSnapshotMatchesExpect_PackageMatch(t *testing.T) {
	exp := domain.ExpectDef{Package: "com.android.settings"}
	if !workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("should match on Package")
	}
}

// TestSnapshotMatchesExpect_PackageMismatch returns false on wrong package.
func TestSnapshotMatchesExpect_PackageMismatch(t *testing.T) {
	exp := domain.ExpectDef{Package: "com.other.app"}
	if workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("should not match on wrong Package")
	}
}

// TestSnapshotMatchesExpect_ClassSuffixMatch returns true when activityName ends
// with the expected suffix.
func TestSnapshotMatchesExpect_ClassSuffixMatch(t *testing.T) {
	exp := domain.ExpectDef{ClassSuffix: "PrivateDnsSettings"}
	if !workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("should match on ClassSuffix")
	}
}

// TestSnapshotMatchesExpect_ClassSuffixMismatch returns false when suffix does not match.
func TestSnapshotMatchesExpect_ClassSuffixMismatch(t *testing.T) {
	exp := domain.ExpectDef{ClassSuffix: "WifiSettings"}
	if workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("should not match on wrong ClassSuffix")
	}
}

// TestSnapshotMatchesExpect_TextContains returns true when a target text contains
// the expected substring.
func TestSnapshotMatchesExpect_TextContains(t *testing.T) {
	exp := domain.ExpectDef{TextContains: "Private DNS"}
	if !workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("should match on TextContains")
	}
}

// TestSnapshotMatchesExpect_TextContains_NotFound returns false when no target
// contains the substring.
func TestSnapshotMatchesExpect_TextContains_NotFound(t *testing.T) {
	exp := domain.ExpectDef{TextContains: "Bluetooth"}
	if workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("should not match: 'Bluetooth' not in snapshot targets")
	}
}

// TestSnapshotMatchesExpect_AllFields_Match returns true when all non-kind
// fields match.
func TestSnapshotMatchesExpect_AllFields_Match(t *testing.T) {
	exp := domain.ExpectDef{
		Kind:         "android.activity.created", // Kind is ignored by snapshot check
		Package:      "com.android.settings",
		ClassSuffix:  "PrivateDnsSettings",
		TextContains: "Save",
	}
	if !workflow.SnapshotMatchesExpect(json.RawMessage(snapshotRaw), exp) {
		t.Error("should match: all state fields present in snapshot")
	}
}

// TestSnapshotMatchesExpect_InvalidJSON returns false on malformed input.
func TestSnapshotMatchesExpect_InvalidJSON(t *testing.T) {
	exp := domain.ExpectDef{Package: "com.android.settings"}
	if workflow.SnapshotMatchesExpect(json.RawMessage(`not-json`), exp) {
		t.Error("should return false on invalid JSON")
	}
}

// TestSnapshotMatchesExpect_EmptyRaw returns false on empty raw payload.
func TestSnapshotMatchesExpect_EmptyRaw(t *testing.T) {
	exp := domain.ExpectDef{Package: "com.android.settings"}
	if workflow.SnapshotMatchesExpect(nil, exp) {
		t.Error("should return false on nil raw")
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
