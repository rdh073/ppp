package accountmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type StartAccountCreationInput struct {
	Kind            string
	DeviceID        string
	PersonaID       string
	PhoneNumber     string
	CaptchaEndpoint string
}

type AccountCreationService struct {
	tasks    TaskControl
	personas store.PersonaStore
	accounts store.AccountStore
	publish  ProjectionPublisher
	baseURL  string
	log      *slog.Logger

	mu   sync.RWMutex
	runs map[string]*domain.AccountCreationRun
}

func NewAccountCreationService(
	tasks TaskControl,
	personas store.PersonaStore,
	accounts store.AccountStore,
	publish ProjectionPublisher,
	baseURL string,
	log *slog.Logger,
) *AccountCreationService {
	return &AccountCreationService{
		tasks:    tasks,
		personas: personas,
		accounts: accounts,
		publish:  publish,
		baseURL:  baseURL,
		log:      log,
		runs:     make(map[string]*domain.AccountCreationRun),
	}
}

func (s *AccountCreationService) List() []*domain.AccountCreationRun {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*domain.AccountCreationRun, 0, len(s.runs))
	for _, run := range s.runs {
		cp := cloneAccountCreationRun(run)
		out = append(out, &cp)
	}
	return out
}

func (s *AccountCreationService) Get(id string) (*domain.AccountCreationRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	run, ok := s.runs[id]
	if !ok {
		return nil, false
	}
	cp := cloneAccountCreationRun(run)
	return &cp, true
}

func (s *AccountCreationService) Start(ctx context.Context, input StartAccountCreationInput) (*domain.AccountCreationRun, error) {
	if input.DeviceID == "" {
		return nil, errors.New("deviceId is required")
	}
	kind := input.Kind
	if kind == "" {
		kind = "google"
	}

	now := time.Now()
	run := &domain.AccountCreationRun{
		ID:        domain.NewAccountCreationID(),
		Kind:      kind,
		DeviceID:  input.DeviceID,
		PersonaID: input.PersonaID,
		Status:    domain.AccountCreationStatusRunning,
		Phase:     domain.AccountCreationPhaseGoogle,
		CreatedAt: now,
		UpdatedAt: now,
	}

	baseURL := s.baseURL
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}
	captchaEndpoint := input.CaptchaEndpoint
	if captchaEndpoint == "" {
		captchaEndpoint = baseURL + "/captcha/solve"
	}

	s.mu.Lock()
	s.runs[run.ID] = run
	s.mu.Unlock()
	s.publishRun(run, "upsert")

	go s.run(context.Background(), run, input.PhoneNumber, captchaEndpoint, baseURL)

	cp := cloneAccountCreationRun(run)
	return &cp, nil
}

func (s *AccountCreationService) run(ctx context.Context, run *domain.AccountCreationRun, phoneNumber, captchaEndpoint, baseURL string) {
	var persona *domain.Persona
	if run.PersonaID != "" {
		if p, ok := s.personas.GetByID(run.PersonaID); ok {
			persona = &p
		}
	}

	req := AccountCreationRunRequest{
		Run:             run,
		Persona:         persona,
		PhoneNumber:     phoneNumber,
		CaptchaEndpoint: captchaEndpoint,
		BaseURL:         baseURL,
	}
	deps := AccountCreationRunDeps{
		Tasks:    s.tasks,
		Personas: s.personas,
		Accounts: s.accounts,
	}

	if err := ExecuteAccountCreationRun(ctx, req, deps, s.log); err != nil {
		s.update(run, func(run *domain.AccountCreationRun) {
			run.Status = domain.AccountCreationStatusFailed
			run.Error = err.Error()
		})
		if s.log != nil {
			s.log.Warn("account creation failed", "accountCreationId", run.ID, "reason", err.Error())
		}
		return
	}

	s.update(run, func(run *domain.AccountCreationRun) {
		run.Status = domain.AccountCreationStatusDone
		run.Error = ""
	})
	if s.log != nil {
		s.log.Info("account creation complete", "accountCreationId", run.ID, "kind", run.Kind, "googleAccountId", run.GoogleAccountID)
	}
}

func (s *AccountCreationService) update(run *domain.AccountCreationRun, fn func(*domain.AccountCreationRun)) {
	s.mu.Lock()
	fn(run)
	run.UpdatedAt = time.Now()
	snapshot := cloneAccountCreationRun(run)
	s.mu.Unlock()
	s.publishRun(&snapshot, "upsert")
}

func cloneAccountCreationRun(run *domain.AccountCreationRun) domain.AccountCreationRun {
	if run == nil {
		return domain.AccountCreationRun{}
	}
	return *run
}

func (s *AccountCreationService) publishRun(run *domain.AccountCreationRun, eventType string) {
	if s.publish == nil || run == nil {
		return
	}
	s.publish.PublishProjection(ProjectionEvent{
		Topic:      "account-manager.account-creations",
		Type:       eventType,
		EntityID:   run.ID,
		OccurredAt: time.Now().UTC(),
		Payload:    cloneAccountCreationRun(run),
	})
}

type LoginRunConfig struct {
	Platform        string
	AccountKind     string
	PackagesToClear []string
	WorkflowName    string
}

type StartLoginRunInput struct {
	AccountID string
	Email     string
	Password  string
	DeviceID  string
}

type LoginRunService struct {
	tasks    TaskControl
	accounts store.AccountStore
	adb      AdbPmClearer
	publish  ProjectionPublisher
	log      *slog.Logger
	cfg      LoginRunConfig

	mu   sync.RWMutex
	runs map[string]*domain.LoginRun
}

func NewLoginRunService(
	tasks TaskControl,
	accounts store.AccountStore,
	adb AdbPmClearer,
	publish ProjectionPublisher,
	log *slog.Logger,
	cfg LoginRunConfig,
) *LoginRunService {
	return &LoginRunService{
		tasks:    tasks,
		accounts: accounts,
		adb:      adb,
		publish:  publish,
		log:      log,
		cfg:      cfg,
		runs:     make(map[string]*domain.LoginRun),
	}
}

func (s *LoginRunService) List() []*domain.LoginRun {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*domain.LoginRun, 0, len(s.runs))
	for _, run := range s.runs {
		cp := cloneLoginRun(run)
		out = append(out, &cp)
	}
	return out
}

func (s *LoginRunService) Get(id string) (*domain.LoginRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	run, ok := s.runs[id]
	if !ok {
		return nil, false
	}
	cp := cloneLoginRun(run)
	return &cp, true
}

func (s *LoginRunService) Start(ctx context.Context, input StartLoginRunInput) (*domain.LoginRun, domain.Account, error) {
	if input.DeviceID == "" {
		return nil, domain.Account{}, errors.New("deviceId is required")
	}

	var account domain.Account
	if input.AccountID != "" {
		a, ok := s.accounts.GetByID(input.AccountID)
		if !ok {
			return nil, domain.Account{}, errors.New("account not found: " + input.AccountID)
		}
		account = a
	} else {
		if input.Email == "" || input.Password == "" {
			return nil, domain.Account{}, errors.New("accountId or (email + password) is required")
		}
		account = domain.Account{
			ID:        domain.NewAccountID(),
			Kind:      s.cfg.AccountKind,
			Email:     input.Email,
			Password:  input.Password,
			Status:    domain.AccountStatusDeactive,
			CreatedAt: time.Now(),
		}
		if err := s.accounts.Save(account); err != nil {
			return nil, domain.Account{}, fmt.Errorf("save account: %w", err)
		}
	}

	now := time.Now()
	run := &domain.LoginRun{
		ID:        domain.NewLoginRunID(),
		Platform:  s.cfg.Platform,
		AccountID: account.ID,
		DeviceID:  input.DeviceID,
		Status:    domain.LoginRunStatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	s.runs[run.ID] = run
	s.mu.Unlock()
	s.publishRun(run, "upsert")

	go s.run(context.Background(), run, account)

	cp := cloneLoginRun(run)
	return &cp, account, nil
}

func (s *LoginRunService) run(ctx context.Context, run *domain.LoginRun, account domain.Account) {
	req := LoginRunRequest{
		Account:         account,
		DeviceID:        domain.DeviceID(run.DeviceID),
		AccountKind:     s.cfg.AccountKind,
		PackagesToClear: s.cfg.PackagesToClear,
		WorkflowName:    s.cfg.WorkflowName,
	}
	deps := LoginRunDeps{
		Tasks:    s.tasks,
		Accounts: s.accounts,
		Adb:      s.adb,
	}

	taskID, err := ExecuteLoginRun(ctx, req, deps, s.log)
	if taskID != "" {
		s.updateLoginRun(run, func(run *domain.LoginRun) { run.TaskID = string(taskID) })
	}
	if err != nil {
		s.updateLoginRun(run, func(run *domain.LoginRun) {
			run.Status = domain.LoginRunStatusFailed
			run.Error = err.Error()
		})
		if s.log != nil {
			s.log.Warn("login run failed", "platform", s.cfg.Platform, "loginRunId", run.ID, "reason", err.Error())
		}
		return
	}

	s.updateLoginRun(run, func(run *domain.LoginRun) {
		run.Status = domain.LoginRunStatusDone
		run.Error = ""
	})
	if s.log != nil {
		s.log.Info("login run done", "platform", s.cfg.Platform, "loginRunId", run.ID, "accountId", account.ID, "deviceId", run.DeviceID)
	}
}

func (s *LoginRunService) updateLoginRun(run *domain.LoginRun, fn func(*domain.LoginRun)) {
	s.mu.Lock()
	fn(run)
	run.UpdatedAt = time.Now()
	snapshot := cloneLoginRun(run)
	s.mu.Unlock()
	s.publishRun(&snapshot, "upsert")
}

func cloneLoginRun(run *domain.LoginRun) domain.LoginRun {
	if run == nil {
		return domain.LoginRun{}
	}
	return *run
}

func (s *LoginRunService) publishRun(run *domain.LoginRun, eventType string) {
	if s.publish == nil || run == nil {
		return
	}
	s.publish.PublishProjection(ProjectionEvent{
		Topic:      "account-manager.logins." + s.cfg.Platform,
		Type:       eventType,
		EntityID:   run.ID,
		OccurredAt: time.Now().UTC(),
		Payload:    cloneLoginRun(run),
	})
}
