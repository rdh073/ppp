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

const androidKind domain.EventKind = domain.EventKindScreenChanged
const otherKind domain.EventKind = domain.EventKindAppForeground

func boolPtr(v bool) *bool {
	return &v
}

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

func TestMatchEvent_UI_Match(t *testing.T) {
	event := makeEvent(domain.EventKindScreenChanged, `{
		"packageName": "cn.wps.moffice_eng",
		"className": "cn.wps.moffice.MainActivity",
		"ui": {
			"activeUiKey": "wps.editor.ready",
			"baseScreenKey": "wps.editor",
			"overlayKey": "export.sheet",
			"uiReady": true,
			"focusedTargetKey": "rename.title",
			"forms": [
				{"formKey": "rename.document", "ready": true}
			],
			"buttons": [
				{"buttonKey": "export.pdf.confirm", "enabled": true, "visible": true, "primary": true}
			]
		}
	}`)

	match := domain.EventMatch{
		Kind: domain.EventKindScreenChanged,
		UI: &domain.UiMatch{
			ActiveUIKey:      "wps.editor.ready",
			BaseScreenKey:    "wps.editor",
			OverlayKey:       "export.sheet",
			UIReady:          boolPtr(true),
			FormKey:          "rename.document",
			FormReady:        boolPtr(true),
			ButtonKey:        "export.pdf.confirm",
			ButtonEnabled:    boolPtr(true),
			FocusedTargetKey: "rename.title",
		},
	}

	if !workflow.MatchEvent(match, event) {
		t.Fatal("semantic UI event should match")
	}
}

func TestMatchEvent_UI_Mismatch(t *testing.T) {
	event := makeEvent(domain.EventKindScreenChanged, `{
		"ui": {
			"activeUiKey": "wps.editor.ready",
			"uiReady": true,
			"buttons": [
				{"buttonKey": "export.pdf.confirm", "enabled": false, "visible": true, "primary": true}
			]
		}
	}`)

	match := domain.EventMatch{
		Kind: domain.EventKindScreenChanged,
		UI: &domain.UiMatch{
			ActiveUIKey:   "wps.editor.ready",
			UIReady:       boolPtr(true),
			ButtonKey:     "export.pdf.confirm",
			ButtonEnabled: boolPtr(true),
		},
	}

	if workflow.MatchEvent(match, event) {
		t.Fatal("semantic UI mismatch should not match")
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

func TestSnapshotMatchesExpect_UI_Match(t *testing.T) {
	raw := json.RawMessage(`{
		"snapshotAfter": {
			"packageName": "cn.wps.moffice_eng",
			"activityName": "cn.wps.moffice.MainActivity",
			"targets": [
				{"text": "Export", "packageName": "cn.wps.moffice_eng"},
				{"label": "Confirm export", "packageName": "cn.wps.moffice_eng"}
			],
			"semantic": {
				"activeUiKey": "wps.export.pdf.dialog",
				"baseScreenKey": "wps.editor",
				"overlayKey": "wps.export.pdf.dialog",
				"uiReady": true,
				"focusedTargetKey": "export.filename",
				"forms": [
					{"formKey": "export.pdf", "ready": true}
				],
				"buttons": [
					{"buttonKey": "export.pdf.confirm", "enabled": true, "visible": true, "primary": true}
				]
			}
		}
	}`)

	exp := domain.ExpectDef{
		Package:      "cn.wps.moffice_eng",
		TextContains: "Confirm export",
		UI: &domain.UiMatch{
			ActiveUIKey:      "wps.export.pdf.dialog",
			BaseScreenKey:    "wps.editor",
			OverlayKey:       "wps.export.pdf.dialog",
			UIReady:          boolPtr(true),
			FormKey:          "export.pdf",
			FormReady:        boolPtr(true),
			ButtonKey:        "export.pdf.confirm",
			ButtonEnabled:    boolPtr(true),
			FocusedTargetKey: "export.filename",
		},
	}

	if !workflow.SnapshotMatchesExpect(raw, exp) {
		t.Fatal("snapshot semantic UI should match")
	}
}

func TestSnapshotMatchesExpect_UI_Mismatch(t *testing.T) {
	raw := json.RawMessage(`{
		"snapshotAfter": {
			"packageName": "cn.wps.moffice_eng",
			"activityName": "cn.wps.moffice.MainActivity",
			"semantic": {
				"activeUiKey": "wps.export.pdf.dialog",
				"uiReady": false,
				"buttons": [
					{"buttonKey": "export.pdf.confirm", "enabled": false, "visible": true, "primary": true}
				]
			}
		}
	}`)

	exp := domain.ExpectDef{
		UI: &domain.UiMatch{
			ActiveUIKey:   "wps.export.pdf.dialog",
			UIReady:       boolPtr(true),
			ButtonKey:     "export.pdf.confirm",
			ButtonEnabled: boolPtr(true),
		},
	}

	if workflow.SnapshotMatchesExpect(raw, exp) {
		t.Fatal("snapshot semantic mismatch should not match")
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

func TestMatchExpect_UI(t *testing.T) {
	event := makeEvent(otherKind, `{
		"ui": {
			"activeUiKey": "wps.editor.ready",
			"uiReady": true,
			"buttons": [
				{"buttonKey": "editor.toolbar.export", "enabled": true, "visible": true, "primary": true}
			]
		}
	}`)

	exp := domain.ExpectDef{
		Kind: otherKind,
		UI: &domain.UiMatch{
			ActiveUIKey:   "wps.editor.ready",
			UIReady:       boolPtr(true),
			ButtonKey:     "editor.toolbar.export",
			ButtonEnabled: boolPtr(true),
		},
	}

	if !workflow.MatchExpect(exp, event) {
		t.Fatal("semantic UI expect should match")
	}
}

// --- OR expect tests ---

// TestMatchExpect_Or_FirstClauseMatches verifies that OR advances when the
// first clause matches.
func TestMatchExpect_Or_FirstClauseMatches(t *testing.T) {
	event := makeEvent(androidKind, `{"packageName":"com.android.settings","text":["Saved"]}`)
	exp := domain.ExpectDef{
		Or: []domain.ExpectDef{
			{Kind: androidKind, TextContains: "Saved"},
			{Kind: domain.EventKindNotification, TextContains: "Berhasil"},
		},
	}
	if !workflow.MatchExpect(exp, event) {
		t.Fatal("OR: first clause matches — should return true")
	}
}

// TestMatchExpect_Or_SecondClauseMatches verifies that OR advances when only
// the second clause matches.
func TestMatchExpect_Or_SecondClauseMatches(t *testing.T) {
	event := makeEvent(domain.EventKindNotification, `{"text":["Berhasil disimpan"]}`)
	exp := domain.ExpectDef{
		Or: []domain.ExpectDef{
			{Kind: androidKind, TextContains: "Saved"},
			{Kind: domain.EventKindNotification, TextContains: "Berhasil"},
		},
	}
	if !workflow.MatchExpect(exp, event) {
		t.Fatal("OR: second clause matches — should return true")
	}
}

// TestMatchExpect_Or_NoneMatch verifies that OR returns false when no clause
// matches.
func TestMatchExpect_Or_NoneMatch(t *testing.T) {
	event := makeEvent(androidKind, `{"packageName":"com.android.settings","text":["Network"]}`)
	exp := domain.ExpectDef{
		Or: []domain.ExpectDef{
			{Kind: androidKind, TextContains: "Saved"},
			{Kind: domain.EventKindNotification, TextContains: "Berhasil"},
		},
	}
	if workflow.MatchExpect(exp, event) {
		t.Fatal("OR: no clause matches — should return false")
	}
}

// TestSnapshotMatchesExpect_Or_FirstClauseMatches verifies OR on snapshot check.
func TestSnapshotMatchesExpect_Or_FirstClauseMatches(t *testing.T) {
	raw := json.RawMessage(`{"snapshotAfter":{"packageName":"com.android.settings","activityName":"PrivateDnsSettings","targets":[{"text":"Saved"}]}}`)
	exp := domain.ExpectDef{
		Or: []domain.ExpectDef{
			{Package: "com.android.settings", TextContains: "Saved"},
			{Package: "com.android.mms", TextContains: "Terkirim"},
		},
	}
	if !workflow.SnapshotMatchesExpect(raw, exp) {
		t.Fatal("OR snapshot: first clause matches — should return true")
	}
}

// TestSnapshotMatchesExpect_Or_KindOnlyClausesSkipped verifies that OR clauses
// with only Kind (not a snapshot-observable field) are skipped, and the check
// returns false when no other clause matches.
func TestSnapshotMatchesExpect_Or_KindOnlyClausesSkipped(t *testing.T) {
	raw := json.RawMessage(`{"snapshotAfter":{"packageName":"com.android.settings","targets":[]}}`)
	exp := domain.ExpectDef{
		Or: []domain.ExpectDef{
			{Kind: androidKind},                 // Kind-only — not a snapshot property
			{Kind: domain.EventKindNotification}, // Kind-only — not a snapshot property
		},
	}
	if workflow.SnapshotMatchesExpect(raw, exp) {
		t.Fatal("OR snapshot: Kind-only clauses are not snapshot properties — should return false")
	}
}
