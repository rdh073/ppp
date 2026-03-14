package nodes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	workflowpkg "github.com/autosdk/ppp/server-agent/internal/workflow"
)

const (
	privateDNSHostnameArtifact       = "private_dns_hostname"
	privateDNSStepArtifact           = "private_dns_step"
	privateDNSGoalReachedArtifact    = "goal_reached"
	privateDNSTerminalReasonArtifact = "terminal_reason"
	privateDNSScrollAttemptsArtifact = "private_dns_scroll_attempts"
	privateDNSMaxScrollAttempts      = 5
	settingsPackageName              = "com.android.settings"
)

var (
	privateDNSRowTexts = []string{
		"Private DNS",
	}
	privateDNSHostnameModeTexts = []string{
		"Private DNS provider hostname",
		"Provider hostname",
		"Host name of DNS provider",
		"Designated Private DNS",
	}
	settingsNetworkEntryTexts = []string{
		"Network & internet",
		"Network and Internet",
		"Connections",
		"Connection & sharing",
		"Connection and sharing",
		"More connections",
		"More connectivity options",
	}
	saveButtonTexts = []string{
		"Save",
	}
)

func runAndroidSettingsPrivateDNSWorkflowDecision(input workflowpkg.NodeInput) (workflowpkg.NodeOutput, error) {
	out := workflowpkg.NodeOutput{
		Status:    workflowpkg.NodeStatusSuccess,
		Artifacts: map[string]string{},
	}

	artifacts := input.State.Artifacts
	if artifacts[privateDNSGoalReachedArtifact] == "true" || artifacts["pending_action"] != "" || artifacts["wait_for_events"] != "" {
		return out, nil
	}

	hostname := strings.TrimSpace(artifacts[privateDNSHostnameArtifact])
	if hostname == "" {
		return privateDNSFailure("private_dns_hostname_missing"), nil
	}

	snapshot, err := parseSnapshotRaw(artifacts["last_observe_raw"])
	if err != nil {
		return privateDNSFailure(fmt.Sprintf("private_dns_snapshot_unavailable:%v", err)), nil
	}

	if privateDNSConfigured(snapshot, hostname) {
		out.Artifacts[privateDNSGoalReachedArtifact] = "true"
		out.Artifacts["private_dns_applied_hostname"] = hostname
		out.Artifacts[privateDNSTerminalReasonArtifact] = "private_dns_hostname_applied"
		out.Artifacts[privateDNSStepArtifact] = "completed"
		out.DeleteArtifacts = []string{"pending_action", "wait_for_events"}
		return out, nil
	}

	if !strings.EqualFold(strings.TrimSpace(snapshot.PackageName), settingsPackageName) {
		return queuePrivateDNSAction(out, "open_settings", map[string]any{
			"action": map[string]any{
				"kind": "open_app",
				"target": map[string]any{
					"kind":  "package_name",
					"value": settingsPackageName,
				},
			},
		}), nil
	}

	if saveLabel, ok := findVisibleText(snapshot, saveButtonTexts); ok && privateDNSInputVisible(snapshot) && privateDNSHostnamePresent(snapshot, hostname) {
		return queuePrivateDNSAction(out, "save_hostname", clickActionByText(saveLabel)), nil
	}

	if privateDNSInputVisible(snapshot) {
		return queuePrivateDNSAction(out, "input_hostname", inputTextActionByResourceID("android:id/edit", hostname)), nil
	}

	if optionLabel, ok := findVisibleText(snapshot, privateDNSHostnameModeTexts); ok {
		return queuePrivateDNSAction(out, "select_provider_hostname_mode", clickActionByText(optionLabel)), nil
	}

	if privateDNSLabel, ok := findVisibleText(snapshot, privateDNSRowTexts); ok {
		return queuePrivateDNSAction(out, "open_private_dns", clickActionByText(privateDNSLabel)), nil
	}

	if networkLabel, ok := findVisibleText(snapshot, settingsNetworkEntryTexts); ok {
		return queuePrivateDNSAction(out, "open_network_settings", clickActionByText(networkLabel)), nil
	}

	if scrolls := privateDNSScrollAttempts(artifacts); canScroll(snapshot) && scrolls < privateDNSMaxScrollAttempts {
		out.Artifacts[privateDNSScrollAttemptsArtifact] = strconv.Itoa(scrolls + 1)
		return queuePrivateDNSAction(out, "scroll_for_private_dns", scrollForwardAction()), nil
	}

	return privateDNSFailure("private_dns_target_not_found"), nil
}

func privateDNSFailure(reason string) workflowpkg.NodeOutput {
	return workflowpkg.NodeOutput{
		Status: workflowpkg.NodeStatusFailure,
		Artifacts: map[string]string{
			"resync_reason": reason,
		},
		DeleteArtifacts: []string{"pending_action", "wait_for_events"},
	}
}

func queuePrivateDNSAction(out workflowpkg.NodeOutput, step string, payload map[string]any) workflowpkg.NodeOutput {
	raw, err := json.Marshal(payload)
	if err != nil {
		return privateDNSFailure("private_dns_action_marshal_failed")
	}
	out.Artifacts["pending_action"] = string(raw)
	out.Artifacts[privateDNSStepArtifact] = step
	return out
}

func clickActionByText(text string) map[string]any {
	return map[string]any{
		"action": map[string]any{
			"kind": "click",
			"target": map[string]any{
				"kind":  "text",
				"value": text,
			},
		},
	}
}

func inputTextActionByResourceID(resourceID string, value string) map[string]any {
	return map[string]any{
		"action": map[string]any{
			"kind": "input_text",
			"target": map[string]any{
				"kind":  "resource_id",
				"value": resourceID,
			},
			"inputText": value,
		},
	}
}

func scrollForwardAction() map[string]any {
	return map[string]any{
		"action": map[string]any{
			"kind":      "scroll",
			"direction": "forward",
		},
	}
}

func privateDNSConfigured(snapshot *domain.UiSnapshot, hostname string) bool {
	if snapshot == nil || !strings.EqualFold(strings.TrimSpace(snapshot.PackageName), settingsPackageName) {
		return false
	}
	if privateDNSInputVisible(snapshot) {
		return false
	}
	return privateDNSHostnamePresent(snapshot, hostname)
}

func privateDNSInputVisible(snapshot *domain.UiSnapshot) bool {
	if snapshot == nil {
		return false
	}
	for _, target := range snapshot.Targets {
		if strings.EqualFold(strings.TrimSpace(target.ResourceID), "android:id/edit") {
			return true
		}
	}
	return false
}

func privateDNSHostnamePresent(snapshot *domain.UiSnapshot, hostname string) bool {
	needle := normalizeUIText(hostname)
	if needle == "" || snapshot == nil {
		return false
	}
	for _, target := range snapshot.Targets {
		if normalizeUIText(target.Text) == needle {
			return true
		}
	}
	return false
}

func findVisibleText(snapshot *domain.UiSnapshot, variants []string) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	lookup := make(map[string]struct{}, len(variants))
	for _, variant := range variants {
		lookup[normalizeUIText(variant)] = struct{}{}
	}
	for _, target := range snapshot.Targets {
		if _, ok := lookup[normalizeUIText(target.Text)]; ok {
			return strings.TrimSpace(target.Text), true
		}
	}
	return "", false
}

func canScroll(snapshot *domain.UiSnapshot) bool {
	if snapshot == nil {
		return false
	}
	for _, target := range snapshot.Targets {
		if target.Scrollable {
			return true
		}
	}
	return false
}

func privateDNSScrollAttempts(artifacts map[string]string) int {
	value := strings.TrimSpace(artifacts[privateDNSScrollAttemptsArtifact])
	if value == "" {
		return 0
	}
	attempts, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return attempts
}

func normalizeUIText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
