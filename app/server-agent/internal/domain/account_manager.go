package domain

import "time"

// AccountCreationStatus is the overall status of an account-creation run.
type AccountCreationStatus string

const (
	AccountCreationStatusRunning AccountCreationStatus = "running"
	AccountCreationStatusDone    AccountCreationStatus = "done"
	AccountCreationStatusFailed  AccountCreationStatus = "failed"
)

// AccountCreationPhase tracks which stage the account-creation run is executing.
type AccountCreationPhase string

const (
	AccountCreationPhaseGoogle    AccountCreationPhase = "google"
	AccountCreationPhaseInstagram AccountCreationPhase = "instagram"
)

// AccountCreationRun tracks an end-to-end account creation run: Google account -> Instagram account.
type AccountCreationRun struct {
	ID              string                `json:"id"`
	Kind            string                `json:"kind"` // "google+instagram" | "google"
	DeviceID        string                `json:"deviceId"`
	PersonaID       string                `json:"personaId,omitempty"`
	Status          AccountCreationStatus `json:"status"`
	Phase           AccountCreationPhase  `json:"phase,omitempty"`
	GoogleTaskID    string                `json:"googleTaskId,omitempty"`
	InstagramTaskID string                `json:"instagramTaskId,omitempty"`
	GoogleAccountID string                `json:"googleAccountId,omitempty"`
	Error           string                `json:"error,omitempty"`
	CreatedAt       time.Time             `json:"createdAt"`
	UpdatedAt       time.Time             `json:"updatedAt"`
}

func NewAccountCreationID() string {
	return "acctcreate-" + newID()
}

// LoginRunStatus is the status of a login run.
type LoginRunStatus string

const (
	LoginRunStatusRunning LoginRunStatus = "running"
	LoginRunStatusDone    LoginRunStatus = "done"
	LoginRunStatusFailed  LoginRunStatus = "failed"
)

// LoginRun tracks a single platform account login run (Google or Instagram).
type LoginRun struct {
	ID        string         `json:"id"`
	Platform  string         `json:"platform"` // "google" | "instagram"
	AccountID string         `json:"accountId"`
	DeviceID  string         `json:"deviceId"`
	TaskID    string         `json:"taskId,omitempty"`
	Status    LoginRunStatus `json:"status"`
	Error     string         `json:"error,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

func NewLoginRunID() string {
	return "login-" + newID()
}
