package workflow

import (
	"encoding/json"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// SnapshotMatchesExpect checks whether the snapshotAfter embedded in raw (the
// device.execute response payload) already satisfies exp.
//
// Only the state-observable fields (Package, ClassSuffix, TextContains) are
// evaluated; Kind is an event type, not derivable from a snapshot, so a
// Kind-only ExpectDef always returns false.
//
// Returns false on any parse error or when raw is empty.
func SnapshotMatchesExpect(raw json.RawMessage, exp domain.ExpectDef) bool {
	if len(raw) == 0 || !expectHasSnapshotCriteria(exp) {
		return false
	}
	var result struct {
		SnapshotAfter domain.UiSnapshot `json:"snapshotAfter"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return false
	}
	return matchSnapshot(result.SnapshotAfter, exp.Package, exp.ClassSuffix, exp.TextContains, exp.UI)
}

func expectHasSnapshotCriteria(exp domain.ExpectDef) bool {
	return exp.Package != "" ||
		exp.ClassSuffix != "" ||
		exp.TextContains != "" ||
		!uiMatchEmpty(exp.UI)
}

func matchSnapshot(snapshot domain.UiSnapshot, pkg, classSuffix, textContains string, ui *domain.UiMatch) bool {
	if pkg != "" && !snapshotMatchesPackage(snapshot, pkg) {
		return false
	}
	if classSuffix != "" && !strings.HasSuffix(snapshot.ActivityName, classSuffix) {
		return false
	}
	if textContains != "" && !snapshotContainsText(snapshot, textContains) {
		return false
	}
	if !matchSemanticUI(snapshot.Semantic, ui) {
		return false
	}
	return true
}

func snapshotMatchesPackage(snapshot domain.UiSnapshot, pkg string) bool {
	if snapshot.PackageName == pkg {
		return true
	}
	// Fallback: some accessibility implementations (e.g. Waydroid) report the
	// system UI overlay as the top-level packageName even when the target app
	// is in the foreground. Check whether any target belongs to the expected
	// package as a proxy for "this app is on screen".
	for _, target := range snapshot.Targets {
		if target.PackageName == pkg {
			return true
		}
	}
	return false
}

func snapshotContainsText(snapshot domain.UiSnapshot, substr string) bool {
	for _, target := range snapshot.Targets {
		if strings.Contains(target.Text, substr) || strings.Contains(target.Label, substr) {
			return true
		}
	}
	return false
}

func matchSemanticUI(semantic *domain.UiSemanticState, match *domain.UiMatch) bool {
	if uiMatchEmpty(match) {
		return true
	}
	if semantic == nil {
		return false
	}
	if match.ActiveUIKey != "" && semantic.ActiveUIKey != match.ActiveUIKey {
		return false
	}
	if match.BaseScreenKey != "" && semantic.BaseScreenKey != match.BaseScreenKey {
		return false
	}
	if match.OverlayKey != "" && semantic.OverlayKey != match.OverlayKey {
		return false
	}
	if match.UIReady != nil && semantic.UIReady != *match.UIReady {
		return false
	}
	if match.FocusedTargetKey != "" && semantic.FocusedTargetKey != match.FocusedTargetKey {
		return false
	}
	if !matchForms(semantic.Forms, match.FormKey, match.FormReady) {
		return false
	}
	if !matchButtons(semantic.Buttons, match.ButtonKey, match.ButtonEnabled) {
		return false
	}
	return true
}

func matchForms(forms []domain.UiFormState, formKey string, ready *bool) bool {
	if formKey == "" && ready == nil {
		return true
	}
	for _, form := range forms {
		if formKey != "" && form.FormKey != formKey {
			continue
		}
		if ready != nil && form.Ready != *ready {
			continue
		}
		return true
	}
	return false
}

func matchButtons(buttons []domain.UiButtonState, buttonKey string, enabled *bool) bool {
	if buttonKey == "" && enabled == nil {
		return true
	}
	for _, button := range buttons {
		if buttonKey != "" && button.ButtonKey != buttonKey {
			continue
		}
		if enabled != nil && button.Enabled != *enabled {
			continue
		}
		return true
	}
	return false
}

func uiMatchEmpty(match *domain.UiMatch) bool {
	return match == nil || match.IsEmpty()
}
