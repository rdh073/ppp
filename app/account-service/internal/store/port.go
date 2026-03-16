package store

import (
	"context"

	"github.com/autosdk/ppp/account-service/internal/domain"
)

// AccountStore persists Google and Instagram account records.
// All Save methods are upserts keyed by account ID.
type AccountStore interface {
	// Google
	SaveGoogle(ctx context.Context, a *domain.GoogleAccount) error
	GetGoogle(ctx context.Context, id domain.GoogleAccountID) (*domain.GoogleAccount, error)
	GetGoogleByEmail(ctx context.Context, email string) (*domain.GoogleAccount, error)
	QueryGoogle(ctx context.Context, q GoogleAccountQuery) (GoogleAccountPage, error)

	// Instagram
	SaveInstagram(ctx context.Context, a *domain.InstagramAccount) error
	GetInstagram(ctx context.Context, id domain.InstagramAccountID) (*domain.InstagramAccount, error)
	GetInstagramByUsername(ctx context.Context, username string) (*domain.InstagramAccount, error)
	QueryInstagram(ctx context.Context, q InstagramAccountQuery) (InstagramAccountPage, error)
}

type GoogleAccountQuery struct {
	DeviceID string
	Status   domain.AccountStatus
	Limit    int // default 100, max 500
	Offset   int
}

type GoogleAccountPage struct {
	Items   []*domain.GoogleAccount `json:"items"`
	Total   int                     `json:"total"`
	Limit   int                     `json:"limit"`
	Offset  int                     `json:"offset"`
	HasMore bool                    `json:"hasMore"`
}

type InstagramAccountQuery struct {
	GoogleAccountQuery
	GoogleAccountID domain.GoogleAccountID
}

type InstagramAccountPage struct {
	Items   []*domain.InstagramAccount `json:"items"`
	Total   int                        `json:"total"`
	Limit   int                        `json:"limit"`
	Offset  int                        `json:"offset"`
	HasMore bool                       `json:"hasMore"`
}
