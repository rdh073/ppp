package accountmanager

import (
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type ProjectedAccountStore struct {
	base    store.AccountStore
	publish ProjectionPublisher
}

func NewProjectedAccountStore(base store.AccountStore, publish ProjectionPublisher) *ProjectedAccountStore {
	return &ProjectedAccountStore{
		base:    base,
		publish: publish,
	}
}

func (s *ProjectedAccountStore) Save(account domain.Account) error {
	if err := s.base.Save(account); err != nil {
		return err
	}
	s.publishAccount("upsert", account)
	return nil
}

func (s *ProjectedAccountStore) GetByID(id string) (domain.Account, bool) {
	return s.base.GetByID(id)
}

func (s *ProjectedAccountStore) List(kind, deviceID string) []domain.Account {
	return s.base.List(kind, deviceID)
}

func (s *ProjectedAccountStore) UpdateStatus(id string, status domain.AccountStatus) error {
	if err := s.base.UpdateStatus(id, status); err != nil {
		return err
	}
	account, ok := s.base.GetByID(id)
	if ok {
		s.publishAccount("upsert", account)
	}
	return nil
}

func (s *ProjectedAccountStore) UpdateDeviceID(id, deviceID string) error {
	if err := s.base.UpdateDeviceID(id, deviceID); err != nil {
		return err
	}
	account, ok := s.base.GetByID(id)
	if ok {
		s.publishAccount("upsert", account)
	}
	return nil
}

func (s *ProjectedAccountStore) FindActiveByKindOnDevice(deviceID, kind string) (domain.Account, bool) {
	return s.base.FindActiveByKindOnDevice(deviceID, kind)
}

func (s *ProjectedAccountStore) publishAccount(eventType string, account domain.Account) {
	if s.publish == nil {
		return
	}
	s.publish.PublishProjection(ProjectionEvent{
		Topic:      "account-manager.accounts",
		Type:       eventType,
		EntityID:   account.ID,
		OccurredAt: time.Now().UTC(),
		Payload:    account,
	})
}

type ProjectedPersonaStore struct {
	base    store.PersonaStore
	publish ProjectionPublisher
}

func NewProjectedPersonaStore(base store.PersonaStore, publish ProjectionPublisher) *ProjectedPersonaStore {
	return &ProjectedPersonaStore{
		base:    base,
		publish: publish,
	}
}

func (s *ProjectedPersonaStore) Save(persona domain.Persona) error {
	if err := s.base.Save(persona); err != nil {
		return err
	}
	s.publishPersona("upsert", persona)
	return nil
}

func (s *ProjectedPersonaStore) GetByID(id string) (domain.Persona, bool) {
	return s.base.GetByID(id)
}

func (s *ProjectedPersonaStore) List(kind, status string) []domain.Persona {
	return s.base.List(kind, status)
}

func (s *ProjectedPersonaStore) UpdateStatus(id string, status domain.PersonaStatus) error {
	if err := s.base.UpdateStatus(id, status); err != nil {
		return err
	}
	persona, ok := s.base.GetByID(id)
	if ok {
		s.publishPersona("upsert", persona)
	}
	return nil
}

func (s *ProjectedPersonaStore) Delete(id string) (bool, error) {
	persona, ok := s.base.GetByID(id)
	found, err := s.base.Delete(id)
	if err != nil {
		return false, err
	}
	if found && ok {
		s.publishPersona("delete", persona)
	}
	return found, nil
}

func (s *ProjectedPersonaStore) publishPersona(eventType string, persona domain.Persona) {
	if s.publish == nil {
		return
	}
	s.publish.PublishProjection(ProjectionEvent{
		Topic:      "account-manager.personas",
		Type:       eventType,
		EntityID:   persona.ID,
		OccurredAt: time.Now().UTC(),
		Payload:    persona,
	})
}
