package accountmanager

import (
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type AccountRegistrationInput struct {
	DeviceID        string
	PersonaID       string
	Email           string
	Username        string
	Password        string
	LinkedAccountID string
}

type AccountService struct {
	store store.AccountStore
}

func NewAccountService(store store.AccountStore) *AccountService {
	return &AccountService{store: store}
}

func (s *AccountService) List(kind, deviceID string) []domain.Account {
	return s.store.List(kind, deviceID)
}

func (s *AccountService) Register(kind string, input AccountRegistrationInput) (domain.Account, error) {
	account := domain.Account{
		ID:              domain.NewAccountID(),
		Kind:            kind,
		DeviceID:        input.DeviceID,
		PersonaID:       input.PersonaID,
		Email:           input.Email,
		Username:        input.Username,
		Password:        input.Password,
		LinkedAccountID: input.LinkedAccountID,
		Status:          domain.AccountStatusActive,
		CreatedAt:       time.Now(),
	}
	if err := s.store.Save(account); err != nil {
		return domain.Account{}, err
	}
	return account, nil
}

type CreatePersonaInput struct {
	Kind      string
	FirstName string
	LastName  string
	Gender    string
	BirthDate string
	Email     string
	Username  string
	Password  string
}

type PersonaService struct {
	store store.PersonaStore
}

func NewPersonaService(store store.PersonaStore) *PersonaService {
	return &PersonaService{store: store}
}

func (s *PersonaService) List(kind, status string) []domain.Persona {
	return s.store.List(kind, status)
}

func (s *PersonaService) Get(id string) (domain.Persona, bool) {
	return s.store.GetByID(id)
}

func (s *PersonaService) Create(input CreatePersonaInput) (domain.Persona, error) {
	kind := input.Kind
	if kind == "" {
		kind = "google"
	}

	now := time.Now()
	persona := domain.Persona{
		ID:        domain.NewPersonaID(),
		Kind:      kind,
		FirstName: input.FirstName,
		LastName:  input.LastName,
		Gender:    input.Gender,
		BirthDate: input.BirthDate,
		Email:     input.Email,
		Username:  input.Username,
		Password:  input.Password,
		Status:    domain.PersonaStatusAvailable,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.store.Save(persona); err != nil {
		return domain.Persona{}, err
	}
	return persona, nil
}

func (s *PersonaService) Delete(id string) (bool, error) {
	return s.store.Delete(id)
}
