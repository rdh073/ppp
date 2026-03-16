package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/autosdk/ppp/account-service/internal/domain"
	"github.com/autosdk/ppp/account-service/internal/store"
)

type AccountRegistryUseCase struct {
	accounts store.AccountStore
	log      *slog.Logger
}

func NewAccountRegistry(accounts store.AccountStore, log *slog.Logger) *AccountRegistryUseCase {
	return &AccountRegistryUseCase{accounts: accounts, log: log}
}

func (uc *AccountRegistryUseCase) RegisterGoogle(ctx context.Context, req RegisterGoogleRequest) (*domain.GoogleAccount, error) {
	if req.Email == "" {
		return nil, fmt.Errorf("%w: email is required", ErrValidation)
	}
	if req.Password == "" {
		return nil, fmt.Errorf("%w: password is required", ErrValidation)
	}
	if !strings.Contains(req.Email, "@") {
		return nil, fmt.Errorf("%w: invalid email", ErrValidation)
	}
	status := req.Status
	if status == "" {
		status = domain.AccountStatusActive
	}
	now := time.Now().UTC()
	a := &domain.GoogleAccount{
		ID:        domain.NewGoogleAccountID(),
		Email:     req.Email,
		Password:  req.Password,
		DeviceID:  req.DeviceID,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := uc.accounts.SaveGoogle(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (uc *AccountRegistryUseCase) GetGoogle(ctx context.Context, id domain.GoogleAccountID) (*domain.GoogleAccount, error) {
	return uc.accounts.GetGoogle(ctx, id)
}

func (uc *AccountRegistryUseCase) GetGoogleByEmail(ctx context.Context, email string) (*domain.GoogleAccount, error) {
	return uc.accounts.GetGoogleByEmail(ctx, email)
}

func (uc *AccountRegistryUseCase) QueryGoogle(ctx context.Context, q store.GoogleAccountQuery) (store.GoogleAccountPage, error) {
	return uc.accounts.QueryGoogle(ctx, q)
}

func (uc *AccountRegistryUseCase) UpdateGoogle(ctx context.Context, id domain.GoogleAccountID, req UpdateGoogleRequest) (*domain.GoogleAccount, error) {
	if req.Status != nil {
		if err := validateStatus(*req.Status); err != nil {
			return nil, err
		}
	}
	a, err := uc.accounts.GetGoogle(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Status != nil {
		a.Status = *req.Status
	}
	if req.DeviceID != nil {
		a.DeviceID = *req.DeviceID
	}
	a.UpdatedAt = time.Now().UTC()
	if err := uc.accounts.SaveGoogle(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (uc *AccountRegistryUseCase) RegisterInstagram(ctx context.Context, req RegisterInstagramRequest) (*domain.InstagramAccount, error) {
	if req.Username == "" {
		return nil, fmt.Errorf("%w: username is required", ErrValidation)
	}
	if req.Password == "" {
		return nil, fmt.Errorf("%w: password is required", ErrValidation)
	}
	status := req.Status
	if status == "" {
		status = domain.AccountStatusActive
	}
	now := time.Now().UTC()
	a := &domain.InstagramAccount{
		ID:              domain.NewInstagramAccountID(),
		Username:        req.Username,
		Contact:         req.Contact,
		Password:        req.Password,
		DeviceID:        req.DeviceID,
		GoogleAccountID: req.GoogleAccountID,
		Status:          status,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := uc.accounts.SaveInstagram(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (uc *AccountRegistryUseCase) GetInstagram(ctx context.Context, id domain.InstagramAccountID) (*domain.InstagramAccount, error) {
	return uc.accounts.GetInstagram(ctx, id)
}

func (uc *AccountRegistryUseCase) GetInstagramByUsername(ctx context.Context, username string) (*domain.InstagramAccount, error) {
	return uc.accounts.GetInstagramByUsername(ctx, username)
}

func (uc *AccountRegistryUseCase) QueryInstagram(ctx context.Context, q store.InstagramAccountQuery) (store.InstagramAccountPage, error) {
	return uc.accounts.QueryInstagram(ctx, q)
}

func (uc *AccountRegistryUseCase) UpdateInstagram(ctx context.Context, id domain.InstagramAccountID, req UpdateInstagramRequest) (*domain.InstagramAccount, error) {
	if req.Status != nil {
		if err := validateStatus(*req.Status); err != nil {
			return nil, err
		}
	}
	a, err := uc.accounts.GetInstagram(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.Status != nil {
		a.Status = *req.Status
	}
	if req.DeviceID != nil {
		a.DeviceID = *req.DeviceID
	}
	if req.GoogleAccountID != nil {
		a.GoogleAccountID = *req.GoogleAccountID
	}
	a.UpdatedAt = time.Now().UTC()
	if err := uc.accounts.SaveInstagram(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func validateStatus(s domain.AccountStatus) error {
	switch s {
	case domain.AccountStatusActive, domain.AccountStatusSuspended, domain.AccountStatusBanned:
		return nil
	default:
		return fmt.Errorf("%w: invalid status %q (must be active|suspended|banned)", ErrValidation, s)
	}
}
