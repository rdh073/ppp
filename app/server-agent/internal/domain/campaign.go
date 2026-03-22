package domain

import (
	"fmt"
	"time"
)

// ---- Campaign (account creation) ----

// CampaignStatus is the overall status of an account creation campaign.
type CampaignStatus string

const (
	CampaignStatusRunning CampaignStatus = "running"
	CampaignStatusDone    CampaignStatus = "done"
	CampaignStatusFailed  CampaignStatus = "failed"
)

// CampaignPhase tracks which stage the campaign is executing.
type CampaignPhase string

const (
	CampaignPhaseGoogle    CampaignPhase = "google"
	CampaignPhaseInstagram CampaignPhase = "instagram"
)

// Campaign tracks an end-to-end account creation run: Google account → Instagram account.
type Campaign struct {
	ID              string         `json:"id"`
	Kind            string         `json:"kind"`     // "google+instagram" | "google"
	DeviceID        string         `json:"deviceId"`
	PersonaID       string         `json:"personaId,omitempty"`
	Status          CampaignStatus `json:"status"`
	Phase           CampaignPhase  `json:"phase,omitempty"`
	GoogleTaskID    string         `json:"googleTaskId,omitempty"`
	InstagramTaskID string         `json:"instagramTaskId,omitempty"`
	GoogleAccountID string         `json:"googleAccountId,omitempty"`
	Error           string         `json:"error,omitempty"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

func NewCampaignID() string {
	return "campaign-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// ---- LoginCampaign ----

// LoginCampaignStatus is the status of a login campaign.
type LoginCampaignStatus string

const (
	LoginCampaignStatusRunning LoginCampaignStatus = "running"
	LoginCampaignStatusDone    LoginCampaignStatus = "done"
	LoginCampaignStatusFailed  LoginCampaignStatus = "failed"
)

// LoginCampaign tracks a single platform account login run (Google or Instagram).
type LoginCampaign struct {
	ID        string              `json:"id"`
	Platform  string              `json:"platform"` // "google" | "instagram"
	AccountID string              `json:"accountId"`
	DeviceID  string              `json:"deviceId"`
	TaskID    string              `json:"taskId,omitempty"`
	Status    LoginCampaignStatus `json:"status"`
	Error     string              `json:"error,omitempty"`
	CreatedAt time.Time           `json:"createdAt"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

func NewLoginCampaignID() string {
	return "login-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

// ---- PostCampaign ----

// PostJob tracks a single per-account post job within a PostCampaign.
type PostJob struct {
	ID        string `json:"id"`
	AccountID string `json:"accountId"`
	DeviceID  string `json:"deviceId"`
	TaskID    string `json:"taskId,omitempty"`
	Status    string `json:"status"` // pending|running|done|failed
	Error     string `json:"error,omitempty"`
}

// PostCampaign tracks a batch Instagram post campaign across multiple accounts.
type PostCampaign struct {
	ID          string    `json:"id"`
	ImageSource string    `json:"imageSource"` // "manual"|"ai"
	TextSource  string    `json:"textSource"`  // "manual"|"ai"
	Caption     string    `json:"caption"`
	Jobs        []PostJob `json:"jobs"`
	Status      string    `json:"status"` // running|done|failed
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func NewPostCampaignID() string {
	return "post-" + fmt.Sprintf("%d", time.Now().UnixNano())
}

func NewPostJobID() string {
	return "pjob-" + fmt.Sprintf("%d", time.Now().UnixNano())
}
