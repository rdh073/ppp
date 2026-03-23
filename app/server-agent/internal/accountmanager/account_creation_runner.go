package accountmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type AccountCreationRunRequest struct {
	Run             *domain.AccountCreationRun
	Persona         *domain.Persona // nil if no persona ID provided
	PhoneNumber     string
	CaptchaEndpoint string
	BaseURL         string
}

type AccountCreationRunDeps struct {
	Tasks    TaskControl
	Personas store.PersonaStore
	Accounts store.AccountStore
}

// ExecuteAccountCreationRun runs account creation for the requested kind.
// Supported kinds: "google", "instagram", "google+instagram".
// It mutates req.Run fields (phase/task IDs/account ID) as execution progresses.
func ExecuteAccountCreationRun(
	ctx context.Context,
	req AccountCreationRunRequest,
	deps AccountCreationRunDeps,
	log *slog.Logger,
) error {
	if deps.Tasks == nil {
		return errors.New("tasks dependency is nil")
	}
	if req.Run == nil {
		return errors.New("account creation run is nil")
	}

	run := req.Run

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}

	// Instagram-only: skip Google phase entirely.
	if run.Kind == "instagram" {
		return executeInstagramOnly(ctx, req, deps, baseURL, log)
	}

	// Google or Google+Instagram: run Google phase first.
	return executeGoogleFlow(ctx, req, deps, baseURL, log)
}

// executeInstagramOnly runs a standalone Instagram account creation.
func executeInstagramOnly(
	ctx context.Context,
	req AccountCreationRunRequest,
	deps AccountCreationRunDeps,
	baseURL string,
	log *slog.Logger,
) error {
	run := req.Run
	run.Phase = domain.AccountCreationPhaseInstagram

	igInputs := map[string]string{
		"account_service_endpoint": baseURL,
	}
	if req.PhoneNumber != "" {
		igInputs["phone_number"] = req.PhoneNumber
	}
	if req.Persona != nil {
		if req.Persona.Username != "" {
			igInputs["username"] = req.Persona.Username
		}
		if req.Persona.Password != "" {
			igInputs["password"] = req.Persona.Password
		}
	}

	igTask, err := deps.Tasks.CreateTask(ctx, CreateTaskRequest{
		Goal:           "Create Instagram account for account creation " + run.ID,
		DeviceID:       domain.DeviceID(run.DeviceID),
		WorkflowName:   "instagram-create-script",
		InputArtifacts: igInputs,
	})
	if err != nil {
		return fmt.Errorf("create instagram task: %w", err)
	}
	run.InstagramTaskID = string(igTask.ID)

	igSummary, ok := pollTaskUntilTerminal(ctx, deps.Tasks, igTask.ID, 10*time.Minute, log)
	if !ok || igSummary == nil || igSummary.Task.Status != domain.TaskStatusCompleted {
		if igSummary != nil {
			return fmt.Errorf("instagram task %s: %s", igSummary.Task.ID, igSummary.Task.Status)
		}
		return errors.New("instagram task failed")
	}

	return nil
}

// executeGoogleFlow runs Google account creation, optionally followed by Instagram.
func executeGoogleFlow(
	ctx context.Context,
	req AccountCreationRunRequest,
	deps AccountCreationRunDeps,
	baseURL string,
	log *slog.Logger,
) error {
	run := req.Run
	run.Phase = domain.AccountCreationPhaseGoogle

	captchaEndpoint := req.CaptchaEndpoint
	if captchaEndpoint == "" {
		captchaEndpoint = baseURL + "/captcha/solve"
	}

	// ── Phase 1: Google account ──────────────────────────────────────────────
	persona := req.Persona
	if persona != nil && deps.Personas != nil {
		_ = deps.Personas.UpdateStatus(persona.ID, domain.PersonaStatusInUse)
	}

	workflowName := "google-account-create-auto-script"
	inputs := map[string]string{
		"captcha_endpoint":         captchaEndpoint,
		"account_service_endpoint": baseURL,
	}
	if req.PhoneNumber != "" {
		inputs["phone_number"] = req.PhoneNumber
	}

	if persona != nil {
		workflowName = "google-account-create-script"
		if persona.FirstName != "" {
			inputs["first_name"] = persona.FirstName
		}
		if persona.LastName != "" {
			inputs["last_name"] = persona.LastName
		}
		if persona.Gender != "" {
			inputs["gender"] = persona.Gender
		}
		if persona.BirthDate != "" {
			inputs["birth_date"] = persona.BirthDate
		}
		if persona.Username != "" {
			inputs["username"] = persona.Username
		}
		if persona.Password != "" {
			inputs["password"] = persona.Password
		}
	}

	googleTask, err := deps.Tasks.CreateTask(ctx, CreateTaskRequest{
		Goal:           "Create Google account for account creation " + run.ID,
		DeviceID:       domain.DeviceID(run.DeviceID),
		WorkflowName:   workflowName,
		InputArtifacts: inputs,
	})
	if err != nil {
		if persona != nil && deps.Personas != nil {
			_ = deps.Personas.UpdateStatus(persona.ID, domain.PersonaStatusAvailable)
		}
		return fmt.Errorf("create google task: %w", err)
	}
	run.GoogleTaskID = string(googleTask.ID)

	googleSummary, ok := pollTaskUntilTerminal(ctx, deps.Tasks, googleTask.ID, 10*time.Minute, log)
	if !ok || googleSummary == nil || googleSummary.Task.Status != domain.TaskStatusCompleted {
		if persona != nil && deps.Personas != nil {
			_ = deps.Personas.UpdateStatus(persona.ID, domain.PersonaStatusAvailable)
		}
		if googleSummary != nil {
			return fmt.Errorf("google task %s: %s", googleSummary.Task.ID, googleSummary.Task.Status)
		}
		return errors.New("google task failed")
	}

	if persona != nil && deps.Personas != nil {
		_ = deps.Personas.UpdateStatus(persona.ID, domain.PersonaStatusUsed)
	}

	googleAccountID := googleSummary.OutputArtifacts["accountId"]
	run.GoogleAccountID = googleAccountID

	// ── Phase 2: Instagram account (only for google+instagram) ───────────────
	if run.Kind != "google+instagram" {
		return nil
	}

	run.Phase = domain.AccountCreationPhaseInstagram

	igInputs := map[string]string{
		"account_service_endpoint": baseURL,
	}
	if googleAccountID != "" {
		igInputs["google_account_id"] = googleAccountID
	}
	for _, k := range []string{"email", "password", "username"} {
		if v := googleSummary.OutputArtifacts[k]; v != "" {
			igInputs["google_"+k] = v
		}
	}
	if req.PhoneNumber != "" {
		igInputs["phone_number"] = req.PhoneNumber
	}

	igTask, err := deps.Tasks.CreateTask(ctx, CreateTaskRequest{
		Goal:           "Create Instagram account for account creation " + run.ID,
		DeviceID:       domain.DeviceID(run.DeviceID),
		WorkflowName:   "instagram-create-script",
		InputArtifacts: igInputs,
	})
	if err != nil {
		return fmt.Errorf("create instagram task: %w", err)
	}
	run.InstagramTaskID = string(igTask.ID)

	igSummary, ok := pollTaskUntilTerminal(ctx, deps.Tasks, igTask.ID, 10*time.Minute, log)
	if !ok || igSummary == nil || igSummary.Task.Status != domain.TaskStatusCompleted {
		if igSummary != nil {
			return fmt.Errorf("instagram task %s: %s", igSummary.Task.ID, igSummary.Task.Status)
		}
		return errors.New("instagram task failed")
	}

	return nil
}
