package domain

import "time"

// AccountStatus describes the current state of a created platform account.
type AccountStatus string

const (
	AccountStatusDeactive   AccountStatus = "deactive"    // saved but not logged in
	AccountStatusInProgress AccountStatus = "in_progress" // login workflow running
	AccountStatusActive     AccountStatus = "active"      // logged in on a device
	AccountStatusFailed     AccountStatus = "failed"
	AccountStatusBanned     AccountStatus = "banned"
)

// Account represents a successfully created platform account (Google, Instagram, etc.)
// persisted after a campaign run.
type Account struct {
	ID              string        `json:"id"`
	Kind            string        `json:"kind"`                      // "google" | "instagram"
	DeviceID        string        `json:"deviceId"`
	PersonaID       string        `json:"personaId,omitempty"`
	Email           string        `json:"email,omitempty"`
	Username        string        `json:"username,omitempty"`
	Password        string        `json:"password,omitempty"`
	LinkedAccountID string        `json:"linkedAccountId,omitempty"` // e.g. Instagram → Google account ID
	Status          AccountStatus `json:"status"`
	CreatedAt       time.Time     `json:"createdAt"`
}

func NewAccountID() string { return "acct-" + newID() }
