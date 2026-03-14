package nodes

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	workflowpkg "github.com/autosdk/ppp/server-agent/internal/workflow"
)

const (
	localIdentityBindingGenerateName        = "local_identity.generate_name"
	localIdentityBindingGenerateEmail       = "local_identity.generate_email"
	localIdentityBindingGeneratePassword    = "local_identity.generate_password"
	localIdentityBindingGenerateBirthDate   = "local_identity.generate_birth_date"
	localIdentityBindingGenerateWelcomeMail = "local_identity.generate_welcome_email"
)

type localIdentityWorkflowOptions struct {
	includeWelcomeEmail bool
}

type localIdentityNameResult struct {
	FullName  string `json:"fullName"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Gender    string `json:"gender"`
}

type localIdentityEmailResult struct {
	Email     string `json:"email"`
	LocalPart string `json:"localPart"`
	Domain    string `json:"domain"`
}

type localIdentityPasswordResult struct {
	Password  string `json:"password"`
	Length    int    `json:"length"`
	HasSymbol bool   `json:"hasSymbol"`
}

type localIdentityBirthDateResult struct {
	BirthDate     string `json:"birthDate"`
	Age           int    `json:"age"`
	ReferenceDate string `json:"referenceDate"`
}

type localIdentityWelcomeEmailResult struct {
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Language string `json:"language"`
	Tone     string `json:"tone"`
}

func runWorkflowDecision(input workflowpkg.NodeInput) (workflowpkg.NodeOutput, error) {
	if input.Task == nil {
		return workflowpkg.NodeOutput{Status: workflowpkg.NodeStatusSuccess}, nil
	}

	switch input.Task.WorkflowName {
	case workflowpkg.LocalIdentityProfileWorkflowName:
		return runLocalIdentityWorkflowDecision(input, localIdentityWorkflowOptions{})
	case workflowpkg.LocalIdentityWelcomeEmailWorkflowName:
		return runLocalIdentityWorkflowDecision(input, localIdentityWorkflowOptions{includeWelcomeEmail: true})
	default:
		return workflowpkg.NodeOutput{Status: workflowpkg.NodeStatusSuccess}, nil
	}
}

func runLocalIdentityWorkflowDecision(input workflowpkg.NodeInput, opts localIdentityWorkflowOptions) (workflowpkg.NodeOutput, error) {
	out := workflowpkg.NodeOutput{
		Status:    workflowpkg.NodeStatusSuccess,
		Artifacts: map[string]string{},
	}

	if input.State.Artifacts["last_tool_binding"] != "" {
		out.DeleteArtifacts = append(out.DeleteArtifacts, "tool_result", "tool_error", "last_tool_name", "last_tool_binding")
	} else {
		if toolResult := input.State.Artifacts["tool_result"]; toolResult != "" {
			lastToolName := input.State.Artifacts["last_tool_name"]
			if lastToolName == "" {
				return localIdentityFailure(opts, fmt.Errorf("tool_result present without last_tool_name")), nil
			}
			consumed, err := consumeLocalIdentityToolResult(lastToolName, toolResult)
			if err != nil {
				return localIdentityFailure(opts, err), nil
			}
			for k, v := range consumed {
				out.Artifacts[k] = v
			}
			out.DeleteArtifacts = append(out.DeleteArtifacts, "tool_result", "last_tool_name", "tool_error")
		}

		if toolError := input.State.Artifacts["tool_error"]; toolError != "" {
			lastToolName := input.State.Artifacts["last_tool_name"]
			if lastToolName == "" {
				return localIdentityFailure(opts, fmt.Errorf("tool_error present without last_tool_name")), nil
			}
			consumed, err := consumeLocalIdentityToolFailure(lastToolName, toolError, input.State.Artifacts, opts)
			if err != nil {
				return localIdentityFailure(opts, err), nil
			}
			for k, v := range consumed {
				out.Artifacts[k] = v
			}
			out.DeleteArtifacts = append(out.DeleteArtifacts, "tool_error", "last_tool_name", "tool_result")
		}
	}

	artifacts := mergeArtifacts(input.State.Artifacts, out.Artifacts, out.DeleteArtifacts)
	if artifacts["pending_tool_binding"] != "" || artifacts["pending_tool"] != "" || artifacts["goal_reached"] == "true" {
		return out, nil
	}

	if nextBindingID, ok := nextLocalIdentityBinding(artifacts, opts); ok {
		return queueLocalIdentityToolBinding(out, nextBindingID), nil
	}

	for k, v := range finalizeLocalIdentityArtifacts(artifacts, opts) {
		out.Artifacts[k] = v
	}
	return out, nil
}

func consumeLocalIdentityToolResult(toolName string, raw string) (map[string]string, error) {
	switch toolName {
	case "identity.generate_indonesian_name":
		var result localIdentityNameResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_full_name":  result.FullName,
			"profile_first_name": result.FirstName,
			"profile_last_name":  result.LastName,
			"profile_gender":     result.Gender,
		}, nil
	case "identity.generate_email":
		var result localIdentityEmailResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_email":            result.Email,
			"profile_email_local_part": result.LocalPart,
			"profile_email_domain":     result.Domain,
		}, nil
	case "credential.generate_password":
		var result localIdentityPasswordResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_password":           result.Password,
			"profile_password_length":    strconv.Itoa(result.Length),
			"profile_password_hasSymbol": strconv.FormatBool(result.HasSymbol),
		}, nil
	case "identity.generate_birth_date":
		var result localIdentityBirthDateResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_birth_date":     result.BirthDate,
			"profile_age":            strconv.Itoa(result.Age),
			"profile_reference_date": result.ReferenceDate,
		}, nil
	case "content.generate_welcome_email":
		var result localIdentityWelcomeEmailResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"welcome_email_subject":         result.Subject,
			"welcome_email_body":            result.Body,
			"welcome_email_language":        result.Language,
			"welcome_email_tone":            result.Tone,
			"welcome_email_generation_mode": "llm",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported tool_result source: %s", toolName)
	}
}

func consumeLocalIdentityToolFailure(
	toolName string,
	toolError string,
	artifacts map[string]string,
	opts localIdentityWorkflowOptions,
) (map[string]string, error) {
	if !opts.includeWelcomeEmail {
		return nil, fmt.Errorf("unsupported tool_error source: %s", toolName)
	}
	switch toolName {
	case "content.generate_welcome_email":
		subject, body := buildFallbackWelcomeEmail(artifacts)
		return map[string]string{
			"welcome_email_subject":         subject,
			"welcome_email_body":            body,
			"welcome_email_language":        "id",
			"welcome_email_tone":            "professional_warm",
			"welcome_email_generation_mode": "fallback_template",
			"welcome_email_error":           toolError,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported tool_error source: %s", toolName)
	}
}

func nextLocalIdentityBinding(artifacts map[string]string, opts localIdentityWorkflowOptions) (bindingID string, ok bool) {
	switch {
	case artifacts["profile_full_name"] == "":
		return localIdentityBindingGenerateName, true
	case artifacts["profile_email"] == "":
		return localIdentityBindingGenerateEmail, true
	case artifacts["profile_password"] == "":
		return localIdentityBindingGeneratePassword, true
	case artifacts["profile_birth_date"] == "":
		return localIdentityBindingGenerateBirthDate, true
	case opts.includeWelcomeEmail && artifacts["welcome_email_subject"] == "":
		return localIdentityBindingGenerateWelcomeMail, true
	default:
		return "", false
	}
}

func finalizeLocalIdentityArtifacts(artifacts map[string]string, opts localIdentityWorkflowOptions) map[string]string {
	final := map[string]string{
		"goal_reached":  "true",
		"profile_ready": "true",
	}
	if opts.includeWelcomeEmail {
		final["terminal_reason"] = "local_identity_welcome_email_ready"
		final["welcome_email_ready"] = "true"
		return final
	}
	final["terminal_reason"] = "local_identity_profile_ready"
	return final
}

func queueLocalIdentityToolBinding(out workflowpkg.NodeOutput, bindingID string) workflowpkg.NodeOutput {
	out.Artifacts["pending_tool_binding"] = bindingID
	return out
}

func localIdentityFailure(opts localIdentityWorkflowOptions, err error) workflowpkg.NodeOutput {
	scope := "local identity"
	if opts.includeWelcomeEmail {
		scope = "local identity welcome email"
	}
	return workflowpkg.NodeOutput{
		Status: workflowpkg.NodeStatusFailure,
		Artifacts: map[string]string{
			"resync_reason": fmt.Sprintf("%s decide failed: %v", scope, err),
		},
		DeleteArtifacts: []string{"pending_tool_binding", "tool_result", "last_tool_name", "last_tool_binding", "tool_error"},
	}
}

func buildFallbackWelcomeEmail(artifacts map[string]string) (string, string) {
	fullName := strings.TrimSpace(artifacts["profile_full_name"])
	if fullName == "" {
		fullName = "Pengguna"
	}
	email := strings.TrimSpace(artifacts["profile_email"])

	subject := "Selamat datang di AutoSDK"
	body := fmt.Sprintf(
		"Halo %s,\n\nAkun AutoSDK Anda sudah siap digunakan. Email yang kami siapkan untuk profil ini adalah %s. Silakan lanjutkan verifikasi dan simpan kredensial Anda dengan aman sebelum memulai workflow berikutnya.\n\nJika Anda membutuhkan bantuan, balas email ini dan tim AutoSDK akan membantu Anda.\n\nSalam,\nAutoSDK",
		fullName,
		email,
	)
	return subject, body
}

func mergeArtifacts(base map[string]string, updates map[string]string, deletes []string) map[string]string {
	merged := make(map[string]string, len(base)+len(updates))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range updates {
		merged[k] = v
	}
	for _, k := range deletes {
		delete(merged, k)
	}
	return merged
}
