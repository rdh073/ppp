package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/autosdk/ppp/account-service/internal/domain"
	"github.com/autosdk/ppp/account-service/internal/store"
	"github.com/autosdk/ppp/account-service/internal/usecase"
)

// AccountHandler exposes the account registry over HTTP.
//
// Routes:
//
//	POST   /accounts/google                       register Google account
//	GET    /accounts/google                       query (deviceId, status, limit, offset)
//	GET    /accounts/google/{id}                  get by ID
//	PATCH  /accounts/google/{id}                  update status / deviceId
//	POST   /accounts/instagram                    register Instagram account
//	GET    /accounts/instagram                    query (deviceId, status, googleAccountId, limit, offset)
//	GET    /accounts/instagram/{id}               get by ID
//	PATCH  /accounts/instagram/{id}               update status / deviceId / googleAccountId
//	GET    /accounts/instagram/{id}/google        linked Google account
//	GET    /devices/{deviceId}/accounts           all accounts for a device
//	GET    /healthz                               health check
type AccountHandler struct {
	uc  usecase.AccountRegistry
	log *slog.Logger
}

func NewAccountHandler(uc usecase.AccountRegistry, log *slog.Logger) *AccountHandler {
	return &AccountHandler{uc: uc, log: log}
}

func (h *AccountHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/healthz":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))

	case strings.HasPrefix(path, "/accounts/"):
		h.routeAccounts(w, r, strings.TrimPrefix(path, "/accounts/"))

	case strings.HasPrefix(path, "/devices/"):
		h.routeDevices(w, r, strings.TrimPrefix(path, "/devices/"))

	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *AccountHandler) routeAccounts(w http.ResponseWriter, r *http.Request, sub string) {
	parts := strings.SplitN(sub, "/", 3)
	switch {
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "google":
		h.createGoogle(w, r)
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "google":
		h.queryGoogle(w, r)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "google":
		h.getGoogle(w, r, domain.GoogleAccountID(parts[1]))
	case r.Method == http.MethodPatch && len(parts) == 2 && parts[0] == "google":
		h.updateGoogle(w, r, domain.GoogleAccountID(parts[1]))

	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "instagram":
		h.createInstagram(w, r)
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "instagram":
		h.queryInstagram(w, r)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "instagram":
		h.getInstagram(w, r, domain.InstagramAccountID(parts[1]))
	case r.Method == http.MethodPatch && len(parts) == 2 && parts[0] == "instagram":
		h.updateInstagram(w, r, domain.InstagramAccountID(parts[1]))
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "instagram" && parts[2] == "google":
		h.getInstagramLinkedGoogle(w, r, domain.InstagramAccountID(parts[1]))

	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *AccountHandler) routeDevices(w http.ResponseWriter, r *http.Request, sub string) {
	// /devices/{deviceId}/accounts
	parts := strings.SplitN(sub, "/", 2)
	if r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "accounts" {
		h.listDeviceAccounts(w, r, parts[0])
		return
	}
	writeError(w, http.StatusNotFound, "not found")
}

// --- Google handlers ---

func (h *AccountHandler) createGoogle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		DeviceID string `json:"deviceId"`
		Status   string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := h.uc.RegisterGoogle(r.Context(), usecase.RegisterGoogleRequest{
		Email:    body.Email,
		Password: body.Password,
		DeviceID: body.DeviceID,
		Status:   domain.AccountStatus(body.Status),
	})
	if err != nil {
		h.log.Error("RegisterGoogle failed", "err", err)
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, googleView(a))
}

func (h *AccountHandler) queryGoogle(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()
	page, err := h.uc.QueryGoogle(r.Context(), store.GoogleAccountQuery{
		DeviceID: q.Get("deviceId"),
		Status:   domain.AccountStatus(q.Get("status")),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, googlePageView(page))
}

func (h *AccountHandler) getGoogle(w http.ResponseWriter, r *http.Request, id domain.GoogleAccountID) {
	a, err := h.uc.GetGoogle(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, googleView(a))
}

func (h *AccountHandler) updateGoogle(w http.ResponseWriter, r *http.Request, id domain.GoogleAccountID) {
	var body struct {
		Status   *string `json:"status"`
		DeviceID *string `json:"deviceId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req := usecase.UpdateGoogleRequest{DeviceID: body.DeviceID}
	if body.Status != nil {
		s := domain.AccountStatus(*body.Status)
		req.Status = &s
	}
	a, err := h.uc.UpdateGoogle(r.Context(), id, req)
	if err != nil {
		h.log.Error("UpdateGoogle failed", "err", err)
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, googleView(a))
}

// --- Instagram handlers ---

func (h *AccountHandler) createInstagram(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username        string `json:"username"`
		Contact         string `json:"contact"`
		Password        string `json:"password"`
		DeviceID        string `json:"deviceId"`
		GoogleAccountID string `json:"googleAccountId"`
		Status          string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	a, err := h.uc.RegisterInstagram(r.Context(), usecase.RegisterInstagramRequest{
		Username:        body.Username,
		Contact:         body.Contact,
		Password:        body.Password,
		DeviceID:        body.DeviceID,
		GoogleAccountID: domain.GoogleAccountID(body.GoogleAccountID),
		Status:          domain.AccountStatus(body.Status),
	})
	if err != nil {
		h.log.Error("RegisterInstagram failed", "err", err)
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, instagramView(a))
}

func (h *AccountHandler) queryInstagram(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePagination(r)
	q := r.URL.Query()
	page, err := h.uc.QueryInstagram(r.Context(), store.InstagramAccountQuery{
		GoogleAccountQuery: store.GoogleAccountQuery{
			DeviceID: q.Get("deviceId"),
			Status:   domain.AccountStatus(q.Get("status")),
			Limit:    limit,
			Offset:   offset,
		},
		GoogleAccountID: domain.GoogleAccountID(q.Get("googleAccountId")),
	})
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instagramPageView(page))
}

func (h *AccountHandler) getInstagram(w http.ResponseWriter, r *http.Request, id domain.InstagramAccountID) {
	a, err := h.uc.GetInstagram(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instagramView(a))
}

func (h *AccountHandler) updateInstagram(w http.ResponseWriter, r *http.Request, id domain.InstagramAccountID) {
	var body struct {
		Status          *string `json:"status"`
		DeviceID        *string `json:"deviceId"`
		GoogleAccountID *string `json:"googleAccountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req := usecase.UpdateInstagramRequest{DeviceID: body.DeviceID}
	if body.Status != nil {
		s := domain.AccountStatus(*body.Status)
		req.Status = &s
	}
	if body.GoogleAccountID != nil {
		g := domain.GoogleAccountID(*body.GoogleAccountID)
		req.GoogleAccountID = &g
	}
	a, err := h.uc.UpdateInstagram(r.Context(), id, req)
	if err != nil {
		h.log.Error("UpdateInstagram failed", "err", err)
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instagramView(a))
}

func (h *AccountHandler) getInstagramLinkedGoogle(w http.ResponseWriter, r *http.Request, id domain.InstagramAccountID) {
	ig, err := h.uc.GetInstagram(r.Context(), id)
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	if ig.GoogleAccountID == "" {
		writeError(w, http.StatusNotFound, "no linked google account")
		return
	}
	g, err := h.uc.GetGoogle(r.Context(), ig.GoogleAccountID)
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, googleView(g))
}

func (h *AccountHandler) listDeviceAccounts(w http.ResponseWriter, r *http.Request, deviceID string) {
	gPage, err := h.uc.QueryGoogle(r.Context(), store.GoogleAccountQuery{DeviceID: deviceID, Limit: 500})
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	igPage, err := h.uc.QueryInstagram(r.Context(), store.InstagramAccountQuery{
		GoogleAccountQuery: store.GoogleAccountQuery{DeviceID: deviceID, Limit: 500},
	})
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	google := make([]map[string]any, len(gPage.Items))
	for i, a := range gPage.Items {
		google[i] = googleView(a)
	}
	instagram := make([]map[string]any, len(igPage.Items))
	for i, a := range igPage.Items {
		instagram[i] = instagramView(a)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"google":    google,
		"instagram": instagram,
	})
}

// --- views (password never returned) ---

func googleView(a *domain.GoogleAccount) map[string]any {
	return map[string]any{
		"id":        string(a.ID),
		"email":     a.Email,
		"deviceId":  a.DeviceID,
		"status":    string(a.Status),
		"createdAt": a.CreatedAt,
		"updatedAt": a.UpdatedAt,
	}
}

func instagramView(a *domain.InstagramAccount) map[string]any {
	return map[string]any{
		"id":              string(a.ID),
		"username":        a.Username,
		"contact":         a.Contact,
		"deviceId":        a.DeviceID,
		"googleAccountId": string(a.GoogleAccountID),
		"status":          string(a.Status),
		"createdAt":       a.CreatedAt,
		"updatedAt":       a.UpdatedAt,
	}
}

func googlePageView(p store.GoogleAccountPage) map[string]any {
	items := make([]map[string]any, len(p.Items))
	for i, a := range p.Items {
		items[i] = googleView(a)
	}
	return map[string]any{
		"items":   items,
		"total":   p.Total,
		"limit":   p.Limit,
		"offset":  p.Offset,
		"hasMore": p.HasMore,
	}
}

func instagramPageView(p store.InstagramAccountPage) map[string]any {
	items := make([]map[string]any, len(p.Items))
	for i, a := range p.Items {
		items[i] = instagramView(a)
	}
	return map[string]any{
		"items":   items,
		"total":   p.Total,
		"limit":   p.Limit,
		"offset":  p.Offset,
		"hasMore": p.HasMore,
	}
}
