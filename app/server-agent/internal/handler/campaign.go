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

// ---- CampaignHandler ----

// CampaignHandler manages campaign lifecycle and serves:
//
//	POST /campaigns            — start a campaign
//	GET  /campaigns/{id}       — poll campaign status
type CampaignHandler struct {
	tasks    usecase.TaskControl
	personas store.PersonaStore
	accounts store.AccountStore
	baseURL  string // e.g. "http://localhost:3000" — used as account_service_endpoint for scripts
	log      *slog.Logger

	mu        sync.RWMutex
	campaigns map[string]*domain.Campaign
}

func NewCampaignHandler(
	tasks usecase.TaskControl,
	personas store.PersonaStore,
	accounts store.AccountStore,
	baseURL string,
	log *slog.Logger,
) *CampaignHandler {
	return &CampaignHandler{
		tasks:     tasks,
		personas:  personas,
		accounts:  accounts,
		baseURL:   baseURL,
		log:       log,
		campaigns: make(map[string]*domain.Campaign),
	}
}

func (h *CampaignHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/campaigns")
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "", "/":
		if r.Method == http.MethodPost {
			h.create(w, r)
		} else if r.Method == http.MethodGet {
			h.list(w, r)
		} else {
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

func (h *CampaignHandler) list(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	out := make([]*domain.Campaign, 0, len(h.campaigns))
	for _, c := range h.campaigns {
		cp := *c
		out = append(out, &cp)
	}
	h.mu.RUnlock()
	jsonOK(w, out)
}

func (h *CampaignHandler) get(w http.ResponseWriter, r *http.Request, id string) {
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

func (h *CampaignHandler) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind            string `json:"kind"` // "google+instagram" (default) | "google"
		DeviceID        string `json:"deviceId"`
		PersonaID       string `json:"personaId"`       // optional
		CaptchaEndpoint string `json:"captchaEndpoint"` // optional override
		PhoneNumber     string `json:"phoneNumber"`     // optional (for SMS OTP)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.DeviceID == "" {
		http.Error(w, "deviceId is required", http.StatusBadRequest)
		return
	}
	if body.Kind == "" {
		body.Kind = "google+instagram"
	}

	now := time.Now()
	c := &domain.Campaign{
		ID:        domain.NewCampaignID(),
		Kind:      body.Kind,
		DeviceID:  body.DeviceID,
		PersonaID: body.PersonaID,
		Status:    domain.CampaignStatusRunning,
		Phase:     domain.CampaignPhaseGoogle,
		CreatedAt: now,
		UpdatedAt: now,
	}

	h.mu.Lock()
	h.campaigns[c.ID] = c
	h.mu.Unlock()

	// Resolve base URL for account_service_endpoint (scripts call back to register accounts).
	baseURL := h.baseURL
	if baseURL == "" {
		baseURL = "http://localhost:3000"
	}

	captchaEndpoint := body.CaptchaEndpoint
	if captchaEndpoint == "" {
		captchaEndpoint = baseURL + "/captcha/solve"
	}

	go h.run(context.Background(), c, body.PhoneNumber, captchaEndpoint, baseURL)

	w.WriteHeader(http.StatusAccepted)
	jsonOK(w, map[string]string{"campaignId": c.ID})
}

// run executes the full campaign lifecycle in a goroutine.
func (h *CampaignHandler) run(ctx context.Context, c *domain.Campaign, phoneNumber, captchaEndpoint, baseURL string) {
	var persona *domain.Persona
	if c.PersonaID != "" {
		if p, ok := h.personas.GetByID(c.PersonaID); ok {
			persona = &p
		}
	}

	req := usecase.AccountCreationRequest{
		Campaign:        c,
		Persona:         persona,
		PhoneNumber:     phoneNumber,
		CaptchaEndpoint: captchaEndpoint,
		BaseURL:         baseURL,
	}
	deps := usecase.AccountCreationDeps{
		Tasks:    h.tasks,
		Personas: h.personas,
		Accounts: h.accounts,
	}

	if err := usecase.ExecuteAccountCreationCampaign(ctx, req, deps, h.log); err != nil {
		h.fail(c, err.Error())
		return
	}

	h.update(c, func(c *domain.Campaign) {
		c.Status = domain.CampaignStatusDone
		c.Error = ""
	})
	h.log.Info("campaign complete", "campaignId", c.ID, "kind", c.Kind, "googleAccountId", c.GoogleAccountID)
}

func (h *CampaignHandler) update(c *domain.Campaign, fn func(*domain.Campaign)) {
	h.mu.Lock()
	fn(c)
	c.UpdatedAt = time.Now()
	h.mu.Unlock()
}

func (h *CampaignHandler) fail(c *domain.Campaign, reason string) {
	h.update(c, func(c *domain.Campaign) {
		c.Status = domain.CampaignStatusFailed
		c.Error = reason
	})
	h.log.Warn("campaign failed", "campaignId", c.ID, "reason", reason)
}
