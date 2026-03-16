package usecase

import (
	"context"
	"errors"

	"github.com/autosdk/ppp/account-service/internal/domain"
	"github.com/autosdk/ppp/account-service/internal/store"
)

// ErrValidation is returned by use-case methods for invalid input.
var ErrValidation = errors.New("validation error")

// AccountRegistry is the primary port for the HTTP account handler.
type AccountRegistry interface {
	RegisterGoogle(ctx context.Context, req RegisterGoogleRequest) (*domain.GoogleAccount, error)
	GetGoogle(ctx context.Context, id domain.GoogleAccountID) (*domain.GoogleAccount, error)
	GetGoogleByEmail(ctx context.Context, email string) (*domain.GoogleAccount, error)
	QueryGoogle(ctx context.Context, q store.GoogleAccountQuery) (store.GoogleAccountPage, error)
	UpdateGoogle(ctx context.Context, id domain.GoogleAccountID, req UpdateGoogleRequest) (*domain.GoogleAccount, error)

	RegisterInstagram(ctx context.Context, req RegisterInstagramRequest) (*domain.InstagramAccount, error)
	GetInstagram(ctx context.Context, id domain.InstagramAccountID) (*domain.InstagramAccount, error)
	GetInstagramByUsername(ctx context.Context, username string) (*domain.InstagramAccount, error)
	QueryInstagram(ctx context.Context, q store.InstagramAccountQuery) (store.InstagramAccountPage, error)
	UpdateInstagram(ctx context.Context, id domain.InstagramAccountID, req UpdateInstagramRequest) (*domain.InstagramAccount, error)
}

// Compile-time interface satisfaction check.
var _ AccountRegistry = (*AccountRegistryUseCase)(nil)

type RegisterGoogleRequest struct {
	Email    string
	Password string
	DeviceID string
	Status   domain.AccountStatus // defaults to "active" when empty
}

type RegisterInstagramRequest struct {
	Username        string
	Contact         string
	Password        string
	DeviceID        string
	GoogleAccountID domain.GoogleAccountID
	Status          domain.AccountStatus // defaults to "active" when empty
}

type UpdateGoogleRequest struct {
	Status   *domain.AccountStatus
	DeviceID *string
}

type UpdateInstagramRequest struct {
	Status          *domain.AccountStatus
	DeviceID        *string
	GoogleAccountID *domain.GoogleAccountID
}
