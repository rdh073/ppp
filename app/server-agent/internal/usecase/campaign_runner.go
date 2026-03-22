package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type AccountCreationRequest struct {
	Campaign        *domain.Campaign
	Persona         *domain.Persona // nil if no persona ID provided
	PhoneNumber     string
	CaptchaEndpoint string
	BaseURL         string
}

type AccountCreationDeps struct {
	Tasks    TaskControl
	Personas store.PersonaStore
	Accounts store.AccountStore
}

// ExecuteAccountCreationCampaign runs the 2-phase Google+Instagram account creation.
// It mutates req.Campaign fields (phase/task IDs/account ID) as execution progresses.
func ExecuteAccountCreationCampaign(
	ctx context.Context,
	req AccountCreationRequest,
	deps AccountCreationDeps,
	log *slog.Logger,
) error {
	if deps.Tasks == nil {
		return errors.New("tasks dependency is nil")
	}
	if req.Campaign == nil {
		return errors.New("campaign is nil")
	}

	c := req.Campaign
	c.Phase = domain.CampaignPhaseGoogle

	baseURL := req.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
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
		Goal:           "Create Google account for campaign " + c.ID,
		DeviceID:       domain.DeviceID(c.DeviceID),
		WorkflowName:   workflowName,
		InputArtifacts: inputs,
	})
	if err != nil {
		if persona != nil && deps.Personas != nil {
			_ = deps.Personas.UpdateStatus(persona.ID, domain.PersonaStatusAvailable)
		}
		return fmt.Errorf("create google task: %w", err)
	}
	c.GoogleTaskID = string(googleTask.ID)

	googleSummary, ok := PollTaskUntilTerminal(ctx, deps.Tasks, googleTask.ID, 10*time.Minute, log)
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
	c.GoogleAccountID = googleAccountID

	// ── Phase 2: Instagram account (optional) ────────────────────────────────
	if c.Kind != "google+instagram" {
		return nil
	}

	c.Phase = domain.CampaignPhaseInstagram

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
		Goal:           "Create Instagram account for campaign " + c.ID,
		DeviceID:       domain.DeviceID(c.DeviceID),
		WorkflowName:   "instagram-create-script",
		InputArtifacts: igInputs,
	})
	if err != nil {
		return fmt.Errorf("create instagram task: %w", err)
	}
	c.InstagramTaskID = string(igTask.ID)

	igSummary, ok := PollTaskUntilTerminal(ctx, deps.Tasks, igTask.ID, 10*time.Minute, log)
	if !ok || igSummary == nil || igSummary.Task.Status != domain.TaskStatusCompleted {
		if igSummary != nil {
			return fmt.Errorf("instagram task %s: %s", igSummary.Task.ID, igSummary.Task.Status)
		}
		return errors.New("instagram task failed")
	}

	return nil
}
