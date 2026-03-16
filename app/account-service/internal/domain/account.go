package domain

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type GoogleAccountID    string
type InstagramAccountID string
type AccountStatus      string

const (
	AccountStatusActive    AccountStatus = "active"
	AccountStatusSuspended AccountStatus = "suspended"
	AccountStatusBanned    AccountStatus = "banned"
)

func NewGoogleAccountID() GoogleAccountID {
	return GoogleAccountID("goog-" + newID())
}

func NewInstagramAccountID() InstagramAccountID {
	return InstagramAccountID("ig-" + newID())
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type GoogleAccount struct {
	ID        GoogleAccountID
	Email     string
	Password  string
	DeviceID  string // empty = unbound; plain string (no FK to device registry)
	Status    AccountStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

type InstagramAccount struct {
	ID              InstagramAccountID
	Username        string
	Contact         string // phone or email used at sign-up
	Password        string
	DeviceID        string           // empty = unbound
	GoogleAccountID GoogleAccountID  // empty = not linked
	Status          AccountStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
