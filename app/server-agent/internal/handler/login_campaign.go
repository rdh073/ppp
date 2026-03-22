package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

// LoginPlatformConfig holds platform-specific parameters for login campaigns.
type LoginPlatformConfig struct {
	Platform        string   // "google" | "instagram"
	AccountKind     string   // matches domain.Account.Kind
	PackagesToClear []string // Android packages to pm-clear before login
	WorkflowName    string   // workflow def name for the login task
	PathPrefix      string   // HTTP path prefix, e.g. "/login/google"
}

// GoogleLoginConfig returns the config for Google login campaigns.
func GoogleLoginConfig() LoginPlatformConfig {
	return LoginPlatformConfig{
		Platform:        "google",
		AccountKind:     "google",
		PackagesToClear: []string{"com.google.android.gms", "com.google.android.googlequicksearchbox"},
		WorkflowName:    "google-account-login-script",
		PathPrefix:      "/login/google",
	}
}

// InstagramLoginConfig returns the config for Instagram login campaigns.
func InstagramLoginConfig() LoginPlatformConfig {
	return LoginPlatformConfig{
		Platform:        "instagram",
		AccountKind:     "instagram",
		PackagesToClear: []string{"com.instagram.android"},
		WorkflowName:    "instagram-account-login-script",
		PathPrefix:      "/login/instagram",
	}
}

// LoginCampaignHandler manages platform account login campaigns.
//
//	POST {pathPrefix}           — start a login campaign
//	GET  {pathPrefix}/{id}      — poll status
//	GET  {pathPrefix}           — list all login campaigns
type LoginCampaignHandler struct {
	tasks    usecase.TaskControl
	accounts store.AccountStore
	adb      usecase.AdbPmClearer // optional; nil = skip pm clear
	log      *slog.Logger
	cfg      LoginPlatformConfig

	mu        sync.RWMutex
	campaigns map[string]*domain.LoginCampaign
}

func NewLoginCampaignHandler(
	tasks usecase.TaskControl,
	accounts store.AccountStore,
	adb usecase.AdbPmClearer,
	log *slog.Logger,
	cfg LoginPlatformConfig,
) *LoginCampaignHandler {
	return &LoginCampaignHandler{
		tasks:     tasks,
		accounts:  accounts,
		adb:       adb,
		log:       log,
		cfg:       cfg,
		campaigns: make(map[string]*domain.LoginCampaign),
	}
}

func (h *LoginCampaignHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, h.cfg.PathPrefix)
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "", "/":
		switch r.Method {
		case http.MethodPost:
			h.create(w, r)
		case http.MethodGet:
			h.list(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	default:
		if r.Method == http.MethodGet {
			h.get(w, r, path)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func (h *LoginCampaignHandler) list(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	out := make([]*domain.LoginCampaign, 0, len(h.campaigns))
	for _, c := range h.campaigns {
		cp := *c
		out = append(out, &cp)
	}
	h.mu.RUnlock()
	jsonOK(w, out)
}

func (h *LoginCampaignHandler) get(w http.ResponseWriter, r *http.Request, id string) {
	h.mu.RLock()
	c, ok := h.campaigns[id]
	h.mu.RUnlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	h.mu.RLock()
	cp := *c
	h.mu.RUnlock()
	jsonOK(w, cp)
}

func (h *LoginCampaignHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID string `json:"accountId"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		DeviceID  string `json:"deviceId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.DeviceID == "" {
		http.Error(w, "deviceId is required", http.StatusBadRequest)
		return
	}

	var account domain.Account
	if body.AccountID != "" {
		a, ok := h.accounts.GetByID(body.AccountID)
		if !ok {
			http.Error(w, "account not found: "+body.AccountID, http.StatusNotFound)
			return
		}
		account = a
	} else {
		if body.Email == "" || body.Password == "" {
			http.Error(w, "accountId or (email + password) is required", http.StatusBadRequest)
			return
		}
		account = domain.Account{
			ID:        domain.NewAccountID(),
			Kind:      h.cfg.AccountKind,
			Email:     body.Email,
			Password:  body.Password,
			Status:    domain.AccountStatusDeactive,
			CreatedAt: time.Now(),
		}
		if err := h.accounts.Save(account); err != nil {
			http.Error(w, "save account: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	now := time.Now()
	c := &domain.LoginCampaign{
		ID:        domain.NewLoginCampaignID(),
		Platform:  h.cfg.Platform,
		AccountID: account.ID,
		DeviceID:  body.DeviceID,
		Status:    domain.LoginCampaignStatusRunning,
		CreatedAt: now,
		UpdatedAt: now,
	}

	h.mu.Lock()
	h.campaigns[c.ID] = c
	h.mu.Unlock()

	go h.run(context.Background(), c, account)

	w.WriteHeader(http.StatusAccepted)
	jsonOK(w, map[string]string{"loginCampaignId": c.ID, "accountId": account.ID})
}

func (h *LoginCampaignHandler) run(ctx context.Context, lc *domain.LoginCampaign, account domain.Account) {
	req := usecase.LoginCampaignRequest{
		Account:         account,
		DeviceID:        domain.DeviceID(lc.DeviceID),
		AccountKind:     h.cfg.AccountKind,
		PackagesToClear: h.cfg.PackagesToClear,
		WorkflowName:    h.cfg.WorkflowName,
	}
	deps := usecase.LoginCampaignDeps{
		Tasks:    h.tasks,
		Accounts: h.accounts,
		Adb:      h.adb,
	}

	taskID, err := usecase.ExecuteLoginCampaign(ctx, req, deps, h.log)
	if taskID != "" {
		h.lupdate(lc, func(c *domain.LoginCampaign) { c.TaskID = string(taskID) })
	}
	if err != nil {
		h.failLogin(lc, err.Error())
		return
	}

	h.lupdate(lc, func(c *domain.LoginCampaign) {
		c.Status = domain.LoginCampaignStatusDone
		c.Error = ""
	})
	h.log.Info("login campaign done", "platform", h.cfg.Platform, "campaignId", lc.ID, "accountId", account.ID, "deviceId", lc.DeviceID)
}

func (h *LoginCampaignHandler) lupdate(c *domain.LoginCampaign, fn func(*domain.LoginCampaign)) {
	h.mu.Lock()
	fn(c)
	c.UpdatedAt = time.Now()
	h.mu.Unlock()
}

func (h *LoginCampaignHandler) failLogin(c *domain.LoginCampaign, reason string) {
	h.lupdate(c, func(c *domain.LoginCampaign) {
		c.Status = domain.LoginCampaignStatusFailed
		c.Error = reason
	})
	h.log.Warn("login campaign failed", "platform", h.cfg.Platform, "campaignId", c.ID, "reason", reason)
}
